package wizard

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/truehhart/htmlup/internal/config"
	"github.com/truehhart/htmlup/internal/provider"
	"github.com/truehhart/htmlup/internal/ui"

	_ "github.com/truehhart/htmlup/internal/provider/github"
	_ "github.com/truehhart/htmlup/internal/provider/s3"
)

func TestRun_NonInteractive_AllFromFlags(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")

	tr := true
	res, err := Run(Options{
		Path:           path,
		ProviderName:   "github",
		ProfileName:    "prod",
		Preset:         map[string]string{"repo": "acme/site"},
		NonInteractive: true,
		SetDefault:     &tr,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.IsNew || !res.Default {
		t.Errorf("expected IsNew && Default, got %+v", res)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Default != "github.prod" {
		t.Errorf("default = %q, want %q", cfg.Default, "github.prod")
	}
	prof, ok := cfg.Profile("github", "prod")
	if !ok || prof["repo"] != "acme/site" {
		t.Errorf("profile = %#v, want repo=acme/site", prof)
	}
}

func TestRun_NonInteractive_MissingRequired(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")

	_, err := Run(Options{
		Path:           path,
		ProviderName:   "github",
		ProfileName:    "prod",
		NonInteractive: true,
	})
	if err == nil || !strings.Contains(err.Error(), "missing required field") {
		t.Fatalf("expected missing-required error, got %v", err)
	}
}

func TestRun_NonInteractive_RejectsInvalidPreset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")

	_, err := Run(Options{
		Path:           path,
		ProviderName:   "github",
		ProfileName:    "prod",
		Preset:         map[string]string{"repo": "not-a-valid-repo"},
		NonInteractive: true,
	})
	if err == nil || !strings.Contains(err.Error(), "owner/name") {
		t.Fatalf("expected validation error, got %v", err)
	}
}

func TestRun_NonInteractive_ExistingProfileWithoutForce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")

	tr := true
	if _, err := Run(Options{
		Path: path, ProviderName: "github", ProfileName: "prod",
		Preset: map[string]string{"repo": "acme/site"}, NonInteractive: true, SetDefault: &tr,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	_, err := Run(Options{
		Path: path, ProviderName: "github", ProfileName: "prod",
		Preset: map[string]string{"repo": "acme/other"}, NonInteractive: true,
	})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected already-exists error, got %v", err)
	}
}

func TestRun_NonInteractive_ForceOverwrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")

	tr := true
	if _, err := Run(Options{
		Path: path, ProviderName: "github", ProfileName: "prod",
		Preset: map[string]string{"repo": "acme/site"}, NonInteractive: true, SetDefault: &tr,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	res, err := Run(Options{
		Path: path, ProviderName: "github", ProfileName: "prod",
		Preset: map[string]string{"repo": "acme/other"}, NonInteractive: true, Force: true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.IsNew {
		t.Errorf("expected IsNew=false on overwrite, got true")
	}

	cfg, _ := config.Load(path)
	prof, _ := cfg.Profile("github", "prod")
	if prof["repo"] != "acme/other" {
		t.Errorf("repo = %q, want %q", prof["repo"], "acme/other")
	}
}

func TestRun_NonInteractive_FirstProfileBecomesDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")

	res, err := Run(Options{
		Path: path, ProviderName: "s3", ProfileName: "only",
		Preset: map[string]string{"bucket": "my-bucket"}, NonInteractive: true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Default {
		t.Errorf("first profile should auto-become default")
	}
}

func TestRun_NonInteractive_SecondProfileDoesNotStealDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")

	tr := true
	if _, err := Run(Options{
		Path: path, ProviderName: "github", ProfileName: "prod",
		Preset: map[string]string{"repo": "acme/site"}, NonInteractive: true, SetDefault: &tr,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	res, err := Run(Options{
		Path: path, ProviderName: "s3", ProfileName: "staging",
		Preset: map[string]string{"bucket": "my-bucket"}, NonInteractive: true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Default {
		t.Errorf("second profile shouldn't steal default without explicit opt-in")
	}

	cfg, _ := config.Load(path)
	if cfg.Default != "github.prod" {
		t.Errorf("default = %q, want github.prod (unchanged)", cfg.Default)
	}
}

func TestRun_RejectsUnknownPresetKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")

	_, err := Run(Options{
		Path:           path,
		ProviderName:   "s3",
		ProfileName:    "stg",
		Preset:         map[string]string{"bucket": "my-bucket", "prfix": "site"},
		NonInteractive: true,
	})
	if err == nil || !strings.Contains(err.Error(), "prfix") || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("expected unknown-preset error mentioning prfix, got %v", err)
	}
}

func TestRun_SetDefaultNoOverridesAutopromotion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")

	fl := false
	res, err := Run(Options{
		Path:           path,
		ProviderName:   "github",
		ProfileName:    "prod",
		Preset:         map[string]string{"repo": "acme/site"},
		NonInteractive: true,
		SetDefault:     &fl,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Default {
		t.Errorf("expected Default=false when --set-default=no, got true")
	}

	cfg, _ := config.Load(path)
	if cfg.Default != "" {
		t.Errorf("default = %q, want empty (explicit --set-default=no)", cfg.Default)
	}
}

func TestRun_SetDefaultYesPromotesOverExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")

	tr := true
	if _, err := Run(Options{
		Path: path, ProviderName: "github", ProfileName: "prod",
		Preset: map[string]string{"repo": "acme/site"}, NonInteractive: true, SetDefault: &tr,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	res, err := Run(Options{
		Path: path, ProviderName: "s3", ProfileName: "stg",
		Preset: map[string]string{"bucket": "my-bucket"}, NonInteractive: true, SetDefault: &tr,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Default {
		t.Errorf("expected Default=true with --set-default=yes")
	}

	cfg, _ := config.Load(path)
	if cfg.Default != "s3.stg" {
		t.Errorf("default = %q, want s3.stg", cfg.Default)
	}
}

func TestRun_UnknownProvider(t *testing.T) {
	_, err := Run(Options{
		Path:           filepath.Join(t.TempDir(), "config.toml"),
		ProviderName:   "nope",
		NonInteractive: true,
	})
	if err == nil || !strings.Contains(err.Error(), "unknown provider") {
		t.Fatalf("expected unknown-provider error, got %v", err)
	}
}

func TestRun_Interactive_PromptsForRequired(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")

	// Lines: profile name (default), repo (required).
	// First profile in a fresh config auto-becomes the default, so no
	// set-as-default confirm.
	stdin := strings.NewReader("\nacme/site\n")
	var stdout bytes.Buffer

	res, err := Run(Options{
		Path:         path,
		ProviderName: "github",
		Prompter:     ui.NewPrompter(stdin, &stdout, false),
	})
	if err != nil {
		t.Fatalf("Run: %v\noutput:\n%s", err, stdout.String())
	}
	if res.Profile != "default" {
		t.Errorf("Profile = %q, want default", res.Profile)
	}

	cfg, _ := config.Load(path)
	prof, _ := cfg.Profile("github", "default")
	if prof["repo"] != "acme/site" {
		t.Errorf("repo = %q, want acme/site", prof["repo"])
	}
	if v, ok := prof["branch"]; ok {
		t.Errorf("branch should be unset (blank default), got %q", v)
	}
}

func TestRun_Interactive_RetriesInvalid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")

	// profile name (default), repo (first invalid, then valid).
	stdin := strings.NewReader("\nbogus\nacme/site\n")
	var stdout bytes.Buffer

	if _, err := Run(Options{
		Path:         path,
		ProviderName: "github",
		Prompter:     ui.NewPrompter(stdin, &stdout, false),
	}); err != nil {
		t.Fatalf("Run: %v\noutput:\n%s", err, stdout.String())
	}
	if !strings.Contains(stdout.String(), "invalid") {
		t.Errorf("expected invalid-warning in output, got:\n%s", stdout.String())
	}
}

// Sanity check that both MVP providers implement the schema interface — if a
// future contributor drops the method, this catches it early.
func TestProvidersImplementConfigSchema(t *testing.T) {
	for _, name := range []string{"github", "s3"} {
		p, ok := provider.Get(name)
		if !ok {
			t.Fatalf("provider %q not registered", name)
		}
		schema := p.ConfigSchema()
		if len(schema) == 0 {
			t.Fatalf("provider %q returned empty schema", name)
		}
		seenRequired := false
		for _, f := range schema {
			if f.Required {
				seenRequired = true
			}
		}
		if !seenRequired {
			t.Errorf("provider %q has no required fields — that's surprising", name)
		}
	}
}
