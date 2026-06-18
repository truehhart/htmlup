package github

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/go-github/v72/github"
)

func TestResolveToken(t *testing.T) {
	t.Run("from GITHUB_TOKEN", func(t *testing.T) {
		t.Setenv("GITHUB_TOKEN", "gh-token-123")
		t.Setenv("GH_TOKEN", "")

		token, err := resolveToken(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if token != "gh-token-123" {
			t.Errorf("got %q, want gh-token-123", token)
		}
	})

	t.Run("from GH_TOKEN", func(t *testing.T) {
		t.Setenv("GITHUB_TOKEN", "")
		t.Setenv("GH_TOKEN", "gh-token-456")

		token, err := resolveToken(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if token != "gh-token-456" {
			t.Errorf("got %q, want gh-token-456", token)
		}
	})

	t.Run("GITHUB_TOKEN takes precedence", func(t *testing.T) {
		t.Setenv("GITHUB_TOKEN", "first")
		t.Setenv("GH_TOKEN", "second")

		token, err := resolveToken(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if token != "first" {
			t.Errorf("got %q, want first", token)
		}
	})

	t.Run("error when no token", func(t *testing.T) {
		t.Setenv("GITHUB_TOKEN", "")
		t.Setenv("GH_TOKEN", "")
		t.Setenv("PATH", t.TempDir())

		_, err := resolveToken(context.Background())
		if err == nil {
			t.Fatal("expected error when no token available")
		}
	})
}

func TestHTTPStatusClassification(t *testing.T) {
	ghErr := func(code int) error {
		return &github.ErrorResponse{Response: &http.Response{StatusCode: code}}
	}
	tests := []struct {
		name                   string
		err                    error
		is404, isServer, is409 bool
	}{
		{"nil response", &github.ErrorResponse{}, false, false, false},
		{"transport error", context.DeadlineExceeded, false, false, false},
		{"404", ghErr(http.StatusNotFound), true, false, false},
		{"409", ghErr(http.StatusConflict), false, false, true},
		{"403", ghErr(http.StatusForbidden), false, false, false},
		{"500", ghErr(http.StatusInternalServerError), false, true, false},
		{"503", ghErr(http.StatusServiceUnavailable), false, true, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := is404(tc.err); got != tc.is404 {
				t.Errorf("is404 = %v, want %v", got, tc.is404)
			}
			if got := isServerError(tc.err); got != tc.isServer {
				t.Errorf("isServerError = %v, want %v", got, tc.isServer)
			}
			if got := isStatus(tc.err, http.StatusConflict); got != tc.is409 {
				t.Errorf("isStatus(409) = %v, want %v", got, tc.is409)
			}
		})
	}
}
