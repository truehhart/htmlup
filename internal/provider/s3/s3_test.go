package s3

import "testing"

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
