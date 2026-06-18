package ui

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// newTestOutput returns a plain (color-off) Output plus the buffers backing its
// stdout and stderr streams, so tests can assert on exact bytes.
func newTestOutput() (*Output, *bytes.Buffer, *bytes.Buffer) {
	var out, err bytes.Buffer
	return New(&out, &err, false), &out, &err
}

func TestOutput_ResultGoesToStdout(t *testing.T) {
	o, out, err := newTestOutput()
	o.Result("https://example.com/a", "https://example.com/b")

	if got, want := out.String(), "https://example.com/a\nhttps://example.com/b\n"; got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
	if err.Len() != 0 {
		t.Errorf("stderr should be empty for machine output, got %q", err.String())
	}
}

func TestOutput_StatusGoesToStderr(t *testing.T) {
	cases := []struct {
		name string
		emit func(*Output)
		want string
	}{
		{"info", func(o *Output) { o.Info("published %d files", 3) }, "published 3 files\n"},
		{"success", func(o *Output) { o.Success("done") }, "done\n"},
		{"warn", func(o *Output) { o.Warn("pages may not appear") }, "warning: pages may not appear\n"},
		{"error", func(o *Output) { o.Error(errors.New("boom")) }, "error: boom\n"},
		{"dryrun", func(o *Output) { o.DryRun("would publish 1 file") }, "dry run: would publish 1 file\n"},
		{"progress", func(o *Output) { o.Progress("creating blob foo") }, "creating blob foo\n"},
		{"next", func(o *Output) { o.Next("htmlup publish ./site") }, "next: htmlup publish ./site\n"},
		{"detail", func(o *Output) { o.Detail("https://example.com/a") }, "  https://example.com/a\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o, out, err := newTestOutput()
			tc.emit(o)
			if got := err.String(); got != tc.want {
				t.Errorf("stderr = %q, want %q", got, tc.want)
			}
			if out.Len() != 0 {
				t.Errorf("status must not touch stdout, got %q", out.String())
			}
		})
	}
}

func TestOutput_PlainIsVerbatim(t *testing.T) {
	o, out, _ := newTestOutput()
	o.Plain("default = \"github.prod\"\n")
	if got, want := out.String(), "default = \"github.prod\"\n"; got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
}

// Color is decorative: plain mode must emit no escape codes, styled mode must.
func TestOutput_ColorPolicy(t *testing.T) {
	var out, errBuf bytes.Buffer
	plain := New(&out, &errBuf, false)
	plain.Warn("x")
	if strings.Contains(errBuf.String(), "\x1b[") {
		t.Errorf("plain output must not contain ANSI codes, got %q", errBuf.String())
	}

	errBuf.Reset()
	styled := New(&out, &errBuf, true)
	styled.Warn("x")
	if !strings.Contains(errBuf.String(), "\x1b[") {
		t.Errorf("styled output should contain ANSI codes, got %q", errBuf.String())
	}
	// Even styled, the meaning-bearing word is present in plain text.
	if !strings.Contains(errBuf.String(), "warning") {
		t.Errorf("styled warning still needs the word 'warning', got %q", errBuf.String())
	}
}

func TestPrompter_LineDefaultAndValidate(t *testing.T) {
	var out bytes.Buffer
	// First answer fails validation, second is accepted.
	p := NewPrompter(strings.NewReader("bad\ngood\n"), &out, false)
	v, err := p.Line(LineSpec{
		Label: "Name",
		Validate: func(s string) error {
			if s == "bad" {
				return errors.New("nope")
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Line: %v", err)
	}
	if v != "good" {
		t.Errorf("value = %q, want good", v)
	}
	if !strings.Contains(out.String(), "invalid: nope") {
		t.Errorf("expected invalid notice, got %q", out.String())
	}
}

func TestPrompter_LineKeepsDefaultOnEmpty(t *testing.T) {
	var out bytes.Buffer
	p := NewPrompter(strings.NewReader("\n"), &out, false)
	v, err := p.Line(LineSpec{Label: "Profile", Default: "default", Required: true})
	if err != nil {
		t.Fatalf("Line: %v", err)
	}
	if v != "default" {
		t.Errorf("value = %q, want default", v)
	}
}

func TestPrompter_SelectByIndexAndValue(t *testing.T) {
	var out bytes.Buffer
	opts := []string{"github", "s3"}

	p := NewPrompter(strings.NewReader("2\n"), &out, false)
	if v, err := p.Select("Provider", opts, "github"); err != nil || v != "s3" {
		t.Fatalf("Select by index = %q, %v; want s3", v, err)
	}

	p = NewPrompter(strings.NewReader("github\n"), &out, false)
	if v, err := p.Select("Provider", opts, "s3"); err != nil || v != "github" {
		t.Fatalf("Select by value = %q, %v; want github", v, err)
	}
}

func TestPrompter_SelectSingleAutoPicks(t *testing.T) {
	var out bytes.Buffer
	p := NewPrompter(strings.NewReader(""), &out, false)
	v, err := p.Select("Provider", []string{"github"}, "")
	if err != nil || v != "github" {
		t.Fatalf("single-option select = %q, %v; want github", v, err)
	}
}

func TestPrompter_Confirm(t *testing.T) {
	cases := []struct {
		in   string
		def  bool
		want bool
	}{
		{"y\n", false, true},
		{"n\n", true, false},
		{"\n", true, true},
		{"\n", false, false},
	}
	for _, tc := range cases {
		var out bytes.Buffer
		p := NewPrompter(strings.NewReader(tc.in), &out, false)
		got, err := p.Confirm("ok?", tc.def)
		if err != nil {
			t.Fatalf("Confirm(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("Confirm(%q, def=%v) = %v, want %v", tc.in, tc.def, got, tc.want)
		}
	}
}

func TestPrompter_InteractiveFalseForNonTTY(t *testing.T) {
	p := NewPrompter(strings.NewReader(""), nil, false)
	if p.Interactive() {
		t.Errorf("a string reader is not a TTY; Interactive() should be false")
	}
}

func TestPlural(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		{0, "0 files"},
		{1, "1 file"},
		{2, "2 files"},
	}
	for _, tc := range cases {
		if got := Plural(tc.n, "file", "files"); got != tc.want {
			t.Errorf("Plural(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}

func TestDiscardSwallowsEverything(t *testing.T) {
	o := Discard()
	o.Result("a", "b")
	o.Info("hi")
	o.Success("ok")
	o.Warn("oops")
	// nothing to assert on — Discard returning without panic and writing to
	// io.Discard is the entire contract.
	_ = o
}

// Color mode adds a leading glyph and ANSI; plain mode stays bare so piping and
// NO_COLOR see clean text and the meaning lives entirely in the words.
func TestOutput_SuccessGlyphOnlyInColor(t *testing.T) {
	var out, errBuf bytes.Buffer
	New(&out, &errBuf, true).Success("published 1 file")
	colored := errBuf.String()
	if !strings.Contains(colored, "✓") || !strings.Contains(colored, "\x1b[") {
		t.Errorf("color success should have a ✓ glyph and ANSI, got %q", colored)
	}

	errBuf.Reset()
	New(&out, &errBuf, false).Success("published 1 file")
	if got := errBuf.String(); got != "published 1 file\n" {
		t.Errorf("plain success = %q, want bare text with no glyph", got)
	}
}
