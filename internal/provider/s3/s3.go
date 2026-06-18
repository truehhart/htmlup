package s3

import (
	"context"
	"fmt"
	"io/fs"
	"mime"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	s3svc "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	htmlconfig "github.com/truehhart/htmlup/internal/config"
	"github.com/truehhart/htmlup/internal/fsutil"
	"github.com/truehhart/htmlup/internal/provider"
	"github.com/truehhart/htmlup/internal/ui"
)

func init() {
	provider.Register(&Provider{})
}

type Provider struct {
	bucket string
	prefix string
	region string
}

func (p *Provider) Name() string { return "s3" }

var s3BucketRE = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)

func (p *Provider) ConfigSchema() []provider.ConfigField {
	return []provider.ConfigField{
		{
			Key:      "bucket",
			Label:    "Bucket name",
			Help:     "Target S3 bucket. Exposure (CloudFront, etc.) is your responsibility.",
			Required: true,
			Validate: func(v string) error {
				if !s3BucketRE.MatchString(v) || strings.Contains(v, "..") {
					return fmt.Errorf("not a valid S3 bucket name")
				}
				return nil
			},
		},
		{
			Key:   "prefix",
			Label: "Key prefix (optional)",
			Help:  "Logical folder inside the bucket. Leave blank to upload at the root.",
		},
		{
			Key:   "region",
			Label: "Region (leave blank to use AWS default chain)",
			Help:  "Overrides the region resolved from your AWS config. Leave blank to inherit it.",
			Default: func() string {
				if r := os.Getenv("AWS_REGION"); r != "" {
					return r
				}
				return os.Getenv("AWS_DEFAULT_REGION")
			},
		},
	}
}

func (p *Provider) Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "s3",
		Short: "S3 operations",
	}
	cmd.AddCommand(p.publishCmd())
	return cmd
}

func (p *Provider) publishCmd() *cobra.Command {
	var (
		dryRun      bool
		verbose     bool
		profileName string
	)
	cmd := &cobra.Command{
		Use:   "publish <path>",
		Short: "Publish HTML to S3",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Flags parsed cleanly; from here errors are runtime failures, not
			// misuse, so don't tack the usage screen onto them.
			cmd.SilenceUsage = true
			out := ui.Auto()
			profile, _, err := provider.SelectedProfile(cmd, p.Name(), profileName)
			if err != nil {
				return err
			}
			p.applyProfile(profile, cmd)
			if err := p.validate(); err != nil {
				return err
			}
			files, err := fsutil.ResolveFS(args[0])
			if err != nil {
				return err
			}
			result, err := p.publish(cmd.Context(), provider.Target{
				Files:   files,
				DryRun:  dryRun,
				Verbose: verbose,
				UI:      out,
			})
			if err != nil {
				return err
			}
			out.Result(result.URLs...)
			return nil
		},
	}
	cmd.Flags().StringVar(&p.bucket, "bucket", "", "target S3 bucket")
	cmd.Flags().StringVar(&p.prefix, "prefix", "", "key prefix (logical folder)")
	cmd.Flags().StringVar(&p.region, "region", "", "AWS region override")
	cmd.Flags().StringVar(&profileName, "profile", "", "config profile name to use for s3")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be uploaded without writing")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "per-file progress and SDK detail")
	return cmd
}

func (p *Provider) Publish(ctx context.Context, localPath string, profile htmlconfig.Profile, dryRun, verbose bool, out *ui.Output) (provider.Result, error) {
	p.applyProfile(profile, nil)
	if err := p.validate(); err != nil {
		return provider.Result{}, err
	}
	files, err := fsutil.ResolveFS(localPath)
	if err != nil {
		return provider.Result{}, err
	}
	return p.publish(ctx, provider.Target{
		Files:   files,
		DryRun:  dryRun,
		Verbose: verbose,
		UI:      out,
	})
}

func (p *Provider) applyProfile(profile htmlconfig.Profile, cmd *cobra.Command) {
	if profile == nil {
		return
	}
	if v := profile["bucket"]; v != "" && !provider.FlagChanged(cmd, "bucket") {
		p.bucket = v
	}
	if v, ok := profile["prefix"]; ok && !provider.FlagChanged(cmd, "prefix") {
		p.prefix = v
	}
	if v := profile["region"]; v != "" && !provider.FlagChanged(cmd, "region") {
		p.region = v
	}
}

func (p *Provider) validate() error {
	if p.bucket == "" {
		return fmt.Errorf("--bucket is required (set it with --bucket or a config profile)")
	}
	return nil
}

func (p *Provider) publish(ctx context.Context, t provider.Target) (provider.Result, error) {
	var opts []func(*config.LoadOptions) error
	if p.region != "" {
		opts = append(opts, config.WithRegion(p.region))
	}
	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return provider.Result{}, fmt.Errorf("loading AWS config: %w", err)
	}

	client := s3svc.NewFromConfig(cfg)

	entries, err := collectKeys(t.Files, p.prefix)
	if err != nil {
		return provider.Result{}, err
	}
	if len(entries) == 0 {
		return provider.Result{}, fmt.Errorf("no files to publish")
	}

	urls := make([]string, len(entries))
	for i, e := range entries {
		urls[i] = s3ObjectURL(p.bucket, cfg.Region, e.key)
	}

	if t.DryRun {
		t.UI.DryRun("would upload %s to s3://%s", ui.Plural(len(entries), "file", "files"), p.bucket)
		for _, e := range entries {
			t.UI.Detail("s3://%s/%s", p.bucket, e.key)
		}
		return provider.Result{URLs: urls}, nil
	}

	// Upload concurrently, mirroring the GitHub backend's bounded fan-out: a
	// 100-file site over the wire is otherwise gated on round-trip latency.
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(10)
	for _, e := range entries {
		g.Go(func() error {
			return p.uploadObject(gctx, client, t.Files, e, t.Verbose, t.UI)
		})
	}
	if err := g.Wait(); err != nil {
		return provider.Result{}, err
	}

	t.UI.Success("uploaded %s to %s", ui.Plural(len(urls), "file", "files"), s3URL(p.bucket, cfg.Region, p.prefix))

	return provider.Result{URLs: urls}, nil
}

type s3Entry struct {
	// filePath is the path within the source fs.FS; key is the destination
	// object key (filePath under the configured prefix).
	filePath string
	key      string
}

// collectKeys walks the source tree into the object keys to upload, applying
// the prefix. It does not read file contents — uploadObject streams each file
// at upload time.
func collectKeys(files fs.FS, prefix string) ([]s3Entry, error) {
	var entries []s3Entry
	err := fs.WalkDir(files, ".", func(filePath string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		key := filePath
		if prefix != "" {
			key = path.Join(prefix, filePath)
		}
		entries = append(entries, s3Entry{filePath: filePath, key: key})
		return nil
	})
	return entries, err
}

// uploadObject streams a single file to S3, inferring its content type from the
// extension.
func (p *Provider) uploadObject(ctx context.Context, client *s3svc.Client, files fs.FS, e s3Entry, verbose bool, out *ui.Output) error {
	f, err := files.Open(e.filePath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	stat, err := f.Stat()
	if err != nil {
		return err
	}

	contentType := mime.TypeByExtension(filepath.Ext(e.filePath))
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	if verbose {
		out.Progress("uploading s3://%s/%s (%s)", p.bucket, e.key, contentType)
	}

	_, err = client.PutObject(ctx, &s3svc.PutObjectInput{
		Bucket:        aws.String(p.bucket),
		Key:           aws.String(e.key),
		Body:          f,
		ContentType:   aws.String(contentType),
		ContentLength: aws.Int64(stat.Size()),
	})
	if err != nil {
		return fmt.Errorf("uploading %s: %w", e.key, err)
	}
	return nil
}

func s3URL(bucket, region, prefix string) string {
	u := fmt.Sprintf("https://%s.s3.%s.amazonaws.com/", bucket, regionOrDefault(region))
	if prefix != "" {
		u += encodeKeyPath(prefix) + "/"
	}
	return u
}

// s3ObjectURL is the virtual-hosted–style URL for a single uploaded object.
func s3ObjectURL(bucket, region, key string) string {
	return fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", bucket, regionOrDefault(region), encodeKeyPath(key))
}

// encodeKeyPath percent-encodes each "/"-separated segment of an object key so
// the emitted URL is valid for keys with spaces or reserved characters (#, ?,
// +, non-ASCII). The S3 key itself is stored unencoded — only its URL form is.
func encodeKeyPath(key string) string {
	segments := strings.Split(key, "/")
	for i, s := range segments {
		segments[i] = url.PathEscape(s)
	}
	return strings.Join(segments, "/")
}

// regionOrDefault falls back to us-east-1, the region whose endpoint S3 serves
// when none is configured.
func regionOrDefault(region string) string {
	if region == "" {
		return "us-east-1"
	}
	return region
}
