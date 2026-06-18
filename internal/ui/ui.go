// Package ui owns every user-facing string htmlup emits. It is the single
// import surface for output and prompts, so the CLI speaks with one voice and
// future changes to wording, styling, or stream routing happen in one place.
//
// The contract it enforces:
//
//   - stdout carries only machine-readable command results (the published URLs,
//     a config dump, the version). It stays plain and scriptable.
//   - stderr carries human status: info, success, warnings, dry-run previews,
//     verbose progress, and next-step hints — plus interactive prompts.
//
// Styling is built on the charmbracelet libraries: lipgloss renders the status
// palette and huh drives interactive prompts (see prompt.go). Both are wrapped
// here so command and provider code never import them directly. Color is
// decorative only — meaning always lives in the words, so NO_COLOR or a piped
// stream loses nothing — and the policy (DetectColor / DetectTTY) is resolved
// once when an Output is constructed.
package ui

import (
	"fmt"
	"io"
	"os"
)

// Output is the sink for all non-interactive user-facing text. Construct one
// per command with New (or Auto for the process stdio) and pass it down into
// provider code instead of writing to os.Stdout / os.Stderr directly.
type Output struct {
	out, err io.Writer
	color    bool
	st       styles
}

// New builds an Output over explicit streams. color is decorative styling only;
// callers usually pass DetectColor(stderr) so it follows the terminal.
func New(stdout, stderr io.Writer, color bool) *Output {
	return &Output{out: stdout, err: stderr, color: color, st: newStyles(stderr, color)}
}

// Auto wires an Output to the process stdio, enabling color only on a terminal
// stderr with NO_COLOR unset.
func Auto() *Output {
	return New(os.Stdout, os.Stderr, DetectColor(os.Stderr))
}

// Discard is an Output that drops all human status and machine results. Use it
// in tests that exercise code paths without asserting on what they print.
func Discard() *Output { return New(io.Discard, io.Discard, false) }

// Prompter returns a Prompter that reads from in and renders to the same stderr
// stream (and color policy) as this Output, so prompts and status share a voice.
func (o *Output) Prompter(in io.Reader) *Prompter {
	return NewPrompter(in, o.err, o.color)
}

// --- stdout: stable, machine-readable results ---

// Result writes one result line per argument to stdout. This is the CLI's
// scriptable output; nothing decorative ever lands on this stream.
func (o *Output) Result(lines ...string) {
	for _, l := range lines {
		fprintln(o.out, l)
	}
}

// Plain writes s to stdout verbatim (no added newline) for already-formatted
// machine output such as a config dump.
func (o *Output) Plain(s string) { _, _ = fmt.Fprint(o.out, s) }

// --- stderr: human status ---

// Info reports neutral status.
func (o *Output) Info(format string, a ...any) {
	fprintln(o.err, fmt.Sprintf(format, a...))
}

// Success reports a completed operation: a green line led by a ✓ on a color
// terminal, plain text otherwise — the wording alone still reads as success.
func (o *Output) Success(format string, a ...any) {
	fprintln(o.err, o.st.successPrefix+o.st.successBody.Render(fmt.Sprintf(format, a...)))
}

// Warn reports a recoverable problem or degraded outcome. It is a status line,
// not a returned error — fatal conditions are returned as errors instead.
func (o *Output) Warn(format string, a ...any) {
	fprintln(o.err, o.st.warnPrefix+fmt.Sprintf(format, a...))
}

// Error renders a returned error at the top level: a red line led by a ✗ on a
// color terminal. It is the one place errors are printed — providers return
// them, they don't print.
func (o *Output) Error(err error) {
	fprintln(o.err, o.st.errPrefix+err.Error())
}

// DryRun reports an action that would happen but did not.
func (o *Output) DryRun(format string, a ...any) {
	fprintln(o.err, o.st.dryRunPrefix+fmt.Sprintf(format, a...))
}

// Progress reports verbose-only per-item work. Callers gate it on --verbose.
func (o *Output) Progress(format string, a ...any) {
	fprintln(o.err, o.st.dim.Render(fmt.Sprintf(format, a...)))
}

// Next suggests a follow-up command. The argument is the bare command; callers
// pass it without backticks since terminal output isn't markdown.
func (o *Output) Next(command string) {
	fprintln(o.err, o.st.nextPrefix+command)
}

// Detail writes an indented continuation line under the preceding status, e.g.
// each URL in a dry-run preview or each object key being uploaded.
func (o *Output) Detail(format string, a ...any) {
	fprintln(o.err, "  "+fmt.Sprintf(format, a...))
}

// Plural renders a count with correct singular/plural wording, e.g.
// Plural(1, "file", "files") → "1 file" and Plural(3, "file", "files") → "3 files".
func Plural(n int, singular, plural string) string {
	if n == 1 {
		return "1 " + singular
	}
	return fmt.Sprintf("%d %s", n, plural)
}

// fprintf / fprintln write to w, discarding the write error: ui writes to a
// terminal or a test buffer, where a failed write isn't recoverable and would
// surface elsewhere. Centralizing the discard keeps every call site clean and
// satisfies errcheck without scattering `_, _ =` across the package.
func fprintf(w io.Writer, format string, a ...any) { _, _ = fmt.Fprintf(w, format, a...) }
func fprintln(w io.Writer, s string)               { _, _ = fmt.Fprintln(w, s) }
