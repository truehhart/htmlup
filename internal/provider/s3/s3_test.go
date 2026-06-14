package s3

import (
	"testing"
	"testing/fstest"
)

func TestCollectKeys(t *testing.T) {
	files := fstest.MapFS{
		"index.html":    &fstest.MapFile{Data: []byte("<html>")},
		"css/style.css": &fstest.MapFile{Data: []byte("body{}")},
	}

	t.Run("no prefix", func(t *testing.T) {
		entries, err := collectKeys(files, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 2 {
			t.Fatalf("got %d entries, want 2", len(entries))
		}
		// fs.WalkDir visits lexically: css/style.css before index.html.
		if entries[0].filePath != "css/style.css" || entries[0].key != "css/style.css" {
			t.Errorf("entries[0] = %+v, want filePath/key css/style.css", entries[0])
		}
		if entries[1].key != "index.html" {
			t.Errorf("entries[1].key = %q, want index.html", entries[1].key)
		}
	})

	t.Run("with prefix", func(t *testing.T) {
		entries, err := collectKeys(files, "sites/demo")
		if err != nil {
			t.Fatal(err)
		}
		if entries[0].filePath != "css/style.css" {
			t.Errorf("entries[0].filePath = %q, want css/style.css (unprefixed source path)", entries[0].filePath)
		}
		if entries[0].key != "sites/demo/css/style.css" {
			t.Errorf("entries[0].key = %q, want sites/demo/css/style.css", entries[0].key)
		}
	})

	t.Run("empty fs", func(t *testing.T) {
		entries, err := collectKeys(fstest.MapFS{}, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Errorf("got %d entries, want 0", len(entries))
		}
	})
}

func TestS3URL(t *testing.T) {
	tests := []struct {
		name   string
		bucket string
		region string
		prefix string
		want   string
	}{
		{"us-east-1", "my-bucket", "us-east-1", "", "https://my-bucket.s3.us-east-1.amazonaws.com/"},
		{"eu-west-1", "my-bucket", "eu-west-1", "", "https://my-bucket.s3.eu-west-1.amazonaws.com/"},
		{"with prefix", "my-bucket", "us-east-1", "sites/demo", "https://my-bucket.s3.us-east-1.amazonaws.com/sites/demo/"},
		{"empty region defaults", "my-bucket", "", "", "https://my-bucket.s3.us-east-1.amazonaws.com/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s3URL(tt.bucket, tt.region, tt.prefix)
			if got != tt.want {
				t.Errorf("s3URL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestS3ObjectURL(t *testing.T) {
	tests := []struct {
		name   string
		bucket string
		region string
		key    string
		want   string
	}{
		{"basic", "my-bucket", "us-east-1", "index.html", "https://my-bucket.s3.us-east-1.amazonaws.com/index.html"},
		{"nested key", "my-bucket", "eu-west-1", "reports/q3.html", "https://my-bucket.s3.eu-west-1.amazonaws.com/reports/q3.html"},
		{"empty region defaults", "my-bucket", "", "a.html", "https://my-bucket.s3.us-east-1.amazonaws.com/a.html"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s3ObjectURL(tt.bucket, tt.region, tt.key)
			if got != tt.want {
				t.Errorf("s3ObjectURL() = %q, want %q", got, tt.want)
			}
		})
	}
}
