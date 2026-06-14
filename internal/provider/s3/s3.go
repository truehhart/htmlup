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
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	s3svc "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/truehhart/htmlup/internal/fsutil"
	"github.com/truehhart/htmlup/internal/provider"
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
		dryRun  bool
		verbose bool
	)
	cmd := &cobra.Command{
		Use:   "publish <path>",
		Short: "Publish HTML to S3",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			files, err := fsutil.ResolveFS(args[0])
			if err != nil {
				return err
			}
			result, err := p.publish(cmd.Context(), provider.Target{
				Files:   files,
				DryRun:  dryRun,
				Verbose: verbose,
			})
			if err != nil {
				return err
			}
			result.PrintURLs()
			return nil
		},
	}
	cmd.Flags().StringVar(&p.bucket, "bucket", "", "target S3 bucket")
	_ = cmd.MarkFlagRequired("bucket")
	cmd.Flags().StringVar(&p.prefix, "prefix", "", "key prefix (logical folder)")
	cmd.Flags().StringVar(&p.region, "region", "", "AWS region override")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be uploaded without writing")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "per-file progress and SDK detail")
	return cmd
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
		for _, e := range entries {
			fmt.Fprintf(os.Stderr, "would upload: s3://%s/%s\n", p.bucket, e.key)
		}
		return provider.Result{URLs: urls}, nil
	}

	// Upload concurrently, mirroring the GitHub backend's bounded fan-out: a
	// 100-file site over the wire is otherwise gated on round-trip latency.
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(10)
	for _, e := range entries {
		g.Go(func() error {
			return p.uploadObject(gctx, client, t.Files, e, t.Verbose)
		})
	}
	if err := g.Wait(); err != nil {
		return provider.Result{}, err
	}

	if t.Verbose {
		fmt.Fprintf(os.Stderr, "uploaded %d files to %s\n", len(urls), s3URL(p.bucket, cfg.Region, p.prefix))
	}

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
func (p *Provider) uploadObject(ctx context.Context, client *s3svc.Client, files fs.FS, e s3Entry, verbose bool) error {
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
		fmt.Fprintf(os.Stderr, "uploading: s3://%s/%s (%s)\n", p.bucket, e.key, contentType)
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
