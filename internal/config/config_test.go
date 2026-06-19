package config

import "testing"

func TestParseAndSelectProfiles(t *testing.T) {
	cfg, err := Parse(`
default = "github.personal"

[providers.github.personal]
repo = "owner/repo"
branch = "main"
dir = "docs"

[providers.s3.reports]
bucket = "bucket"
prefix = "reports/"
region = "us-east-1"
`)
	if err != nil {
		t.Fatal(err)
	}
	provider, profile, ok := cfg.DefaultProviderProfile()
	if !ok || provider != "github" || profile != "personal" {
		t.Fatalf("default = %q.%q, %v; want github.personal true", provider, profile, ok)
	}
	got, selected, err := cfg.ProviderDefault("github", "")
	if err != nil {
		t.Fatal(err)
	}
	if selected != "personal" {
		t.Fatalf("selected profile = %q, want personal", selected)
	}
	if got["repo"] != "owner/repo" || got["branch"] != "main" || got["dir"] != "docs" {
		t.Fatalf("github profile = %#v", got)
	}
}

func TestProviderDefaultUsesOnlyProfile(t *testing.T) {
	cfg, err := Parse(`
default = "github.personal"

[providers.s3.reports]
bucket = "bucket"
`)
	if err != nil {
		t.Fatal(err)
	}
	got, selected, err := cfg.ProviderDefault("s3", "")
	if err != nil {
		t.Fatal(err)
	}
	if selected != "reports" || got["bucket"] != "bucket" {
		t.Fatalf("profile = %q %#v, want reports bucket", selected, got)
	}
}

func TestProviderDefaultFallsBackWhenDefaultProfileIsStale(t *testing.T) {
	cfg, err := Parse(`
default = "s3.gone"

[providers.s3.only]
bucket = "bucket"
`)
	if err != nil {
		t.Fatal(err)
	}
	got, selected, err := cfg.ProviderDefault("s3", "")
	if err != nil {
		t.Fatal(err)
	}
	if selected != "only" || got["bucket"] != "bucket" {
		t.Fatalf("profile = %q %#v, want only bucket", selected, got)
	}
}

func TestPublishDefaultUsesConfiguredDefault(t *testing.T) {
	cfg, err := Parse(`
default = "github.personal"

[providers.github.personal]
repo = "owner/repo"

[providers.s3.reports]
bucket = "bucket"
`)
	if err != nil {
		t.Fatal(err)
	}
	providerName, profileName, profile, ok := cfg.PublishDefault()
	if !ok || providerName != "github" || profileName != "personal" || profile["repo"] != "owner/repo" {
		t.Fatalf("PublishDefault = %q %q %#v %v", providerName, profileName, profile, ok)
	}
}

func TestPublishDefaultUsesOnlyProfileWithoutDefault(t *testing.T) {
	cfg, err := Parse(`
[providers.s3.reports]
bucket = "bucket"
`)
	if err != nil {
		t.Fatal(err)
	}
	providerName, profileName, profile, ok := cfg.PublishDefault()
	if !ok || providerName != "s3" || profileName != "reports" || profile["bucket"] != "bucket" {
		t.Fatalf("PublishDefault = %q %q %#v %v", providerName, profileName, profile, ok)
	}
}

func TestPublishDefaultFallsBackWhenDefaultProfileIsStale(t *testing.T) {
	cfg, err := Parse(`
default = "s3.gone"

[providers.s3.only]
bucket = "bucket"
`)
	if err != nil {
		t.Fatal(err)
	}
	providerName, profileName, profile, ok := cfg.PublishDefault()
	if !ok || providerName != "s3" || profileName != "only" || profile["bucket"] != "bucket" {
		t.Fatalf("PublishDefault = %q %q %#v %v", providerName, profileName, profile, ok)
	}
}

func TestPublishDefaultRequiresDefaultForMultipleProfiles(t *testing.T) {
	cfg, err := Parse(`
[providers.github.personal]
repo = "owner/repo"

[providers.s3.reports]
bucket = "bucket"
`)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, ok := cfg.PublishDefault(); ok {
		t.Fatal("expected no publish default")
	}
}

func TestSet(t *testing.T) {
	cfg := Empty()
	var err error
	cfg, err = Set(cfg, "providers.github.personal.repo", "owner/repo")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err = Set(cfg, "default", "github.personal")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Default != "github.personal" || cfg.Providers["github"]["personal"]["repo"] != "owner/repo" {
		t.Fatalf("cfg = %#v", cfg)
	}
}

func TestSetRejectsInvalidNames(t *testing.T) {
	tests := []string{
		`providers.github.my#prof.repo`,
		`providers.git.hub.personal.repo`,
		`providers.github.personal.repo.name`,
		`default`,
	}
	for _, key := range tests[:3] {
		t.Run(key, func(t *testing.T) {
			if _, err := Set(Empty(), key, "value"); err == nil {
				t.Fatal("expected error")
			}
		})
	}
	if _, err := Set(Empty(), tests[3], "github.my#prof"); err == nil {
		t.Fatal("expected invalid default error")
	}
}

func TestParseRejectsInvalidNames(t *testing.T) {
	tests := []string{
		`[providers.github.my#prof]`,
		"[providers.github.bad.name]\nrepo = \"owner/repo\"",
		"[providers.github.personal]\nrepo.name = \"owner/repo\"",
	}
	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			if _, err := Parse(input); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestTOMLRoundTrip(t *testing.T) {
	cfg, err := Parse(`
default = "s3.reports"

[providers.s3.reports]
bucket = "my bucket"
prefix = "reports/#2026"
`)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Parse(cfg.TOML())
	if err != nil {
		t.Fatalf("round trip parse: %v", err)
	}
	profile := got.Providers["s3"]["reports"]
	// A '#' inside a value must survive (the hand-rolled parser truncated it as a comment).
	if profile["bucket"] != "my bucket" || profile["prefix"] != "reports/#2026" {
		t.Fatalf("round trip = %#v", profile)
	}
}
