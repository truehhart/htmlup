package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/google/go-github/v72/github"
	"golang.org/x/oauth2"
)

// newGitHubClient builds a token-authenticated GitHub client. Auth is owned by
// go-github + oauth2 — see resolveToken for where the token comes from.
func newGitHubClient(ctx context.Context, token string) *github.Client {
	return github.NewClient(
		oauth2.NewClient(ctx, oauth2.StaticTokenSource(
			&oauth2.Token{AccessToken: token},
		)),
	)
}

// httpStatus extracts the HTTP status code from a GitHub API error, reporting
// false when err is not a *github.ErrorResponse (e.g. a transport error).
func httpStatus(err error) (int, bool) {
	var ghErr *github.ErrorResponse
	if errors.As(err, &ghErr) && ghErr.Response != nil {
		return ghErr.Response.StatusCode, true
	}
	return 0, false
}

// isStatus reports whether err is a GitHub API error carrying the given status.
func isStatus(err error, code int) bool {
	c, ok := httpStatus(err)
	return ok && c == code
}

// isServerError reports whether err is a GitHub API 5xx — a server-side fault
// that is worth retrying, unlike a 4xx that signals a bad request.
func isServerError(err error) bool {
	c, ok := httpStatus(err)
	return ok && c >= 500
}

// is404 reports whether err is a GitHub API "not found" response, the signal
// that a resource (a Pages config, a branch ref) does not exist yet.
func is404(err error) bool {
	return isStatus(err, http.StatusNotFound)
}

func resolveToken(ctx context.Context) (string, error) {
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		return token, nil
	}
	if token := os.Getenv("GH_TOKEN"); token != "" {
		return token, nil
	}
	cmdCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cmdCtx, "gh", "auth", "token").Output()
	if err == nil {
		if token := strings.TrimSpace(string(out)); token != "" {
			return token, nil
		}
	}
	return "", fmt.Errorf("no GitHub token found: set GITHUB_TOKEN, GH_TOKEN, or run 'gh auth login'")
}
