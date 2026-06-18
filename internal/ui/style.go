package ui

import (
	"io"
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"golang.org/x/term"
)

// styles is the small lipgloss palette ui applies to human status. Color is
// decorative — the wording (and a leading glyph) carries the meaning — so a
// no-color renderer (off-TTY or NO_COLOR) renders every prefix as plain text
// and every glyph falls back to ASCII (effectively empty).
//
// The static fragments — glyph + label, e.g. "✓ " or "warning: " — are
// pre-rendered once at construction so the hot path is plain string concat
// instead of a lipgloss call per Warn/Error/DryRun.
type styles struct {
	successPrefix string         // "<green>✓</> " in color, "" off
	successBody   lipgloss.Style // green wraps the dynamic body
	warnPrefix    string         // "<yellow>!</> <yellow>warning</>: " or "warning: "
	errPrefix     string         // "<red>✗</> <red>error</>: " or "error: "
	dryRunPrefix  string         // "<cyan>dry run</>: " or "dry run: "
	nextPrefix    string         // "<cyan>next</>: " or "next: "
	dim           lipgloss.Style // verbose progress, prompt help, detail lines
}

// newStyles builds the palette on a renderer bound to a specific writer, so the
// color decision follows that stream. color forces the profile: true keeps full
// color, false flattens to ASCII (the NO_COLOR / non-terminal case).
func newStyles(w io.Writer, color bool) styles {
	r := lipgloss.NewRenderer(w)
	if color {
		r.SetColorProfile(termenv.TrueColor)
	} else {
		r.SetColorProfile(termenv.Ascii)
	}
	const (
		green  = "2"
		yellow = "3"
		red    = "1"
		cyan   = "6"
	)
	bold := func(c string) lipgloss.Style { return r.NewStyle().Bold(true).Foreground(lipgloss.Color(c)) }
	plain := func(c string) lipgloss.Style { return r.NewStyle().Foreground(lipgloss.Color(c)) }

	st := styles{
		successBody: plain(green),
		dim:         r.NewStyle().Faint(true),
	}
	if color {
		st.successPrefix = bold(green).Render("✓") + " "
		st.warnPrefix = bold(yellow).Render("!") + " " + bold(yellow).Render("warning") + ": "
		st.errPrefix = bold(red).Render("✗") + " " + bold(red).Render("error") + ": "
		st.dryRunPrefix = bold(cyan).Render("dry run") + ": "
		st.nextPrefix = bold(cyan).Render("next") + ": "
	} else {
		st.warnPrefix = "warning: "
		st.errPrefix = "error: "
		st.dryRunPrefix = "dry run: "
		st.nextPrefix = "next: "
	}
	return st
}

// DetectColor reports whether decorative color should be enabled for w: only
// when w is a terminal and NO_COLOR (https://no-color.org/) is unset.
func DetectColor(w io.Writer) bool {
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false
	}
	return DetectTTY(w)
}

// DetectTTY reports whether v is an interactive terminal. Used to decide
// whether rich prompting is possible; a non-terminal (pipe, CI, redirected
// file) is not. v is typed any so io.Reader / io.Writer values can be probed
// without casting at the call site — only an underlying *os.File counts.
func DetectTTY(v any) bool {
	f, ok := v.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}
