package github

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/go-github/v72/github"
)

// newTestClient returns a github.Client whose requests hit handler instead of
// the real API, so multi-call GitHub API flows can be exercised offline.
func newTestClient(t *testing.T, handler http.Handler) *github.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	c := github.NewClient(nil)
	c.BaseURL = u
	return c
}
