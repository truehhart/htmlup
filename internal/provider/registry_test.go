package provider

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/spf13/cobra"

	"github.com/truehhart/htmlup/internal/config"
	"github.com/truehhart/htmlup/internal/ui"
)

// siteFS stands in for a prepared (already resolved/encrypted) file set passed
// to PublishConfigured.
var siteFS = fstest.MapFS{"index.html": {Data: []byte("<h1>hi</h1>")}}

func resetRegistry() {
	for k := range registry {
		delete(registry, k)
	}
}

type mockProvider struct {
	name       string
	gotFiles   fs.FS
	gotProfile config.Profile
	gotDryRun  bool
	gotVerbose bool
}

func (m *mockProvider) Name() string                   { return m.name }
func (m *mockProvider) ConfigSchema() []ConfigField    { return nil }
func (m *mockProvider) PublishCommand() *cobra.Command { return &cobra.Command{Use: m.name} }

func (m *mockProvider) Publish(_ context.Context, files fs.FS, profile config.Profile, dryRun, verbose bool, _ *ui.Output) (Result, error) {
	m.gotFiles = files
	m.gotProfile = profile
	m.gotDryRun = dryRun
	m.gotVerbose = verbose
	return Result{URLs: []string{"https://example.test/index.html"}}, nil
}

func TestRegisterAndGet(t *testing.T) {
	t.Cleanup(resetRegistry)

	Register(&mockProvider{name: "test"})

	got, ok := Get("test")
	if !ok {
		t.Fatal("expected provider to be found")
	}
	if got.Name() != "test" {
		t.Errorf("got name %q, want %q", got.Name(), "test")
	}
}

func TestGetUnknown(t *testing.T) {
	t.Cleanup(resetRegistry)

	_, ok := Get("nonexistent")
	if ok {
		t.Fatal("expected provider not to be found")
	}
}

func TestRegisterDuplicate(t *testing.T) {
	t.Cleanup(resetRegistry)

	Register(&mockProvider{name: "dup"})

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on duplicate registration")
		}
	}()
	Register(&mockProvider{name: "dup"})
}

func TestNames(t *testing.T) {
	t.Cleanup(resetRegistry)

	Register(&mockProvider{name: "beta"})
	Register(&mockProvider{name: "alpha"})

	names := Names()
	if len(names) != 2 || names[0] != "alpha" || names[1] != "beta" {
		t.Errorf("got %v, want [alpha beta]", names)
	}
}

func TestAll(t *testing.T) {
	t.Cleanup(resetRegistry)

	Register(&mockProvider{name: "beta"})
	Register(&mockProvider{name: "alpha"})

	all := All()
	if len(all) != 2 {
		t.Fatalf("got %d providers, want 2", len(all))
	}
	if all[0].Name() != "alpha" || all[1].Name() != "beta" {
		t.Errorf("got [%s, %s], want [alpha, beta]", all[0].Name(), all[1].Name())
	}
}

func TestPublishConfigured(t *testing.T) {
	t.Cleanup(resetRegistry)

	p := &mockProvider{name: "mock"}
	Register(p)
	cfg := config.Config{
		Default: "mock.personal",
		Providers: map[string]map[string]config.Profile{
			"mock": {
				"personal": {"repo": "owner/repo"},
			},
		},
	}

	result, err := PublishConfigured(context.Background(), siteFS, cfg, true, true, ui.Discard())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.URLs) != 1 || result.URLs[0] != "https://example.test/index.html" {
		t.Fatalf("result = %#v", result)
	}
	if p.gotFiles == nil || p.gotProfile["repo"] != "owner/repo" || !p.gotDryRun || !p.gotVerbose {
		t.Fatalf("publisher args = files %v profile %#v dry %v verbose %v", p.gotFiles, p.gotProfile, p.gotDryRun, p.gotVerbose)
	}
}

func TestPublishConfiguredErrorsWithoutDefault(t *testing.T) {
	t.Cleanup(resetRegistry)

	Register(&mockProvider{name: "mock"})
	cfg := config.Config{
		Providers: map[string]map[string]config.Profile{
			"mock": {
				"personal": {},
				"work":     {},
			},
		},
	}
	_, err := PublishConfigured(context.Background(), siteFS, cfg, false, false, ui.Discard())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestPublishConfiguredUsesOnlyProfileWithoutDefault(t *testing.T) {
	t.Cleanup(resetRegistry)

	p := &mockProvider{name: "mock"}
	Register(p)
	cfg := config.Config{
		Providers: map[string]map[string]config.Profile{
			"mock": {
				"personal": {"repo": "owner/repo"},
			},
		},
	}

	if _, err := PublishConfigured(context.Background(), siteFS, cfg, false, false, ui.Discard()); err != nil {
		t.Fatal(err)
	}
	if p.gotProfile["repo"] != "owner/repo" {
		t.Fatalf("profile = %#v", p.gotProfile)
	}
}

func TestSelectedProfile(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte(`
default = "mock.personal"

[providers.mock.personal]
repo = "owner/repo"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	root := &cobra.Command{Use: "root"}
	root.PersistentFlags().String("config", configPath, "")
	cmd := &cobra.Command{Use: "child"}
	root.AddCommand(cmd)

	profile, selected, err := SelectedProfile(cmd, "mock", "")
	if err != nil {
		t.Fatal(err)
	}
	if selected != "personal" || profile["repo"] != "owner/repo" {
		t.Fatalf("selected = %q profile = %#v", selected, profile)
	}
}

func TestFlagChanged(t *testing.T) {
	cmd := &cobra.Command{Use: "cmd"}
	cmd.Flags().String("repo", "", "")
	if FlagChanged(cmd, "repo") {
		t.Fatal("flag should not be changed before Set")
	}
	if err := cmd.Flags().Set("repo", "owner/repo"); err != nil {
		t.Fatal(err)
	}
	if !FlagChanged(cmd, "repo") {
		t.Fatal("flag should be changed after Set")
	}
	if FlagChanged(nil, "repo") {
		t.Fatal("nil command should not report changed flags")
	}
}
