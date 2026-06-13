package provider

import (
	"testing"

	"github.com/spf13/cobra"
)

func resetRegistry() {
	for k := range registry {
		delete(registry, k)
	}
}

type mockProvider struct {
	name string
}

func (m *mockProvider) Name() string            { return m.name }
func (m *mockProvider) Command() *cobra.Command { return &cobra.Command{Use: m.name} }

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
