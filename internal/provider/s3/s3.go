package s3

import (
	"context"
	"fmt"
	"io/fs"
	"mime"
	"os"
	"path"
	"path/filepath"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	s3svc "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/spf13/cobra"

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

	var urls []string
	err = fs.WalkDir(t.Files, ".", func(filePath string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		key := filePath
		if p.prefix != "" {
			key = path.Join(p.prefix, filePath)
		}
		objURL := s3ObjectURL(p.bucket, cfg.Region, key)

		if t.DryRun {
			fmt.Fprintf(os.Stderr, "would upload: s3://%s/%s\n", p.bucket, key)
			urls = append(urls, objURL)
			return nil
		}

		f, err := t.Files.Open(filePath)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()

		stat, err := f.Stat()
		if err != nil {
			return err
		}

		contentType := mime.TypeByExtension(filepath.Ext(filePath))
		if contentType == "" {
			contentType = "application/octet-stream"
		}

		if t.Verbose {
			fmt.Fprintf(os.Stderr, "uploading: s3://%s/%s (%s)\n", p.bucket, key, contentType)
		}

		_, err = client.PutObject(ctx, &s3svc.PutObjectInput{
			Bucket:        aws.String(p.bucket),
			Key:           aws.String(key),
			Body:          f,
			ContentType:   aws.String(contentType),
			ContentLength: aws.Int64(stat.Size()),
		})
		if err != nil {
			return fmt.Errorf("uploading %s: %w", key, err)
		}

		urls = append(urls, objURL)
		return nil
	})
	if err != nil {
		return provider.Result{}, err
	}

	if len(urls) == 0 {
		return provider.Result{}, fmt.Errorf("no files to publish")
	}

	if t.Verbose {
		fmt.Fprintf(os.Stderr, "uploaded %d files to %s\n", len(urls), s3URL(p.bucket, cfg.Region, p.prefix))
	}

	return provider.Result{URLs: urls}, nil
}

func s3URL(bucket, region, prefix string) string {
	if region == "" {
		region = "us-east-1"
	}
	u := fmt.Sprintf("https://%s.s3.%s.amazonaws.com/", bucket, region)
	if prefix != "" {
		u += prefix + "/"
	}
	return u
}

// s3ObjectURL is the virtual-hosted–style URL for a single uploaded object.
func s3ObjectURL(bucket, region, key string) string {
	if region == "" {
		region = "us-east-1"
	}
	return fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", bucket, region, key)
}
