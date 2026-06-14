package buildinfo

import (
	"runtime/debug"
	"strings"
	"testing"
)

func TestResolve_LdflagsWin(t *testing.T) {
	bi := &debug.BuildInfo{
		Main: debug.Module{Version: "v9.9.9"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "deadbeef"},
			{Key: "vcs.time", Value: "2020-01-01T00:00:00Z"},
		},
	}
	got := resolve("1.2.3", "abc123", "2026-06-14", bi, true)
	if got.Version != "1.2.3" || got.Commit != "abc123" || got.Date != "2026-06-14" {
		t.Fatalf("stamped values should win, got %+v", got)
	}
}

func TestResolve_FallbackToVCS(t *testing.T) {
	bi := &debug.BuildInfo{
		Main: debug.Module{Version: "(devel)"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "cafef00d"},
			{Key: "vcs.time", Value: "2026-06-14T08:00:00Z"},
			{Key: "vcs.modified", Value: "true"},
		},
	}
	got := resolve(devVersion, "", "", bi, true)
	if got.Commit != "cafef00d" {
		t.Errorf("want commit from vcs, got %q", got.Commit)
	}
	if got.Date != "2026-06-14T08:00:00Z" {
		t.Errorf("want date from vcs, got %q", got.Date)
	}
	if !got.Modified {
		t.Error("want modified=true")
	}
	if got.Version != devVersion {
		t.Errorf("want dev version, got %q", got.Version)
	}
}

func TestResolve_ModuleVersion(t *testing.T) {
	bi := &debug.BuildInfo{Main: debug.Module{Version: "v0.1.3"}}
	got := resolve(devVersion, "", "", bi, true)
	if got.Version != "v0.1.3" {
		t.Fatalf("want module version, got %q", got.Version)
	}
}

func TestResolve_NoBuildInfo(t *testing.T) {
	got := resolve("", "", "", nil, false)
	if got.Version != devVersion {
		t.Fatalf("want dev fallback, got %q", got.Version)
	}
}

func TestInfoString(t *testing.T) {
	s := Info{Version: "1.2.3", Commit: "abc", Date: "2026", Modified: true}.String()
	for _, want := range []string{"htmlup 1.2.3", "commit: abc (modified)", "built:  2026"} {
		if !strings.Contains(s, want) {
			t.Errorf("String() missing %q:\n%s", want, s)
		}
	}
	if bare := (Info{Version: devVersion}).String(); strings.Contains(bare, "commit") {
		t.Errorf("no commit line expected for bare version, got %q", bare)
	}
}
