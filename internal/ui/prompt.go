package ui

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"
	"golang.org/x/term"
)

// ErrAborted is returned when the user cancels an interactive prompt (Ctrl+C or
// Esc in a huh form, or declining a "really?" confirmation). It is a clean
// cancel, not a failure: the top-level command detects it and exits quietly
// without rendering an error.
var ErrAborted = errors.New("aborted")

// Prompter is the one interactive-input surface. On an interactive color
// terminal it drives charmbracelet/huh for a polished form experience; off a
// TTY or under NO_COLOR it falls back to a plain line-based reader (the same
// behavior tests and pipes rely on). Either way the API is identical, so the
// wizard and github setup don't care which backend runs.
type Prompter struct {
	in    io.Reader
	out   io.Writer
	r     *bufio.Reader // plain fallback reader
	theme *huh.Theme
	rich  bool // use huh: interactive terminal with color enabled
	tty   bool // input is a terminal (regardless of color)
}

// NewPrompter builds a Prompter over explicit streams. It reads from in and
// renders to out; color selects huh (true) over the plain fallback when in is a
// terminal — so NO_COLOR (color=false) keeps the plain prompts even on a TTY.
func NewPrompter(in io.Reader, out io.Writer, color bool) *Prompter {
	tty := DetectTTY(in)
	return &Prompter{
		in:    in,
		out:   out,
		r:     bufio.NewReader(in),
		theme: huh.ThemeCharm(),
		rich:  tty && color,
		tty:   tty,
	}
}

// Interactive reports whether input is a terminal. Callers that prompt only
// when a human can answer (e.g. an optional confirmation) gate on this; a flow
// that has already committed to reading piped input ignores it.
func (p *Prompter) Interactive() bool { return p.tty }

// LineSpec describes a single free-text prompt.
type LineSpec struct {
	Label    string
	Help     string // one-line guidance shown above the prompt; optional
	Default  string // pre-filled value; empty input keeps it
	Required bool   // empty input (with no default) re-prompts
	Validate func(string) error
}

// Line reads one line, applying the default and validation. Empty input keeps
// the default; a required field with no default re-prompts until satisfied.
func (p *Prompter) Line(spec LineSpec) (string, error) {
	if !p.rich {
		return p.linePlain(spec)
	}

	value := spec.Default
	input := huh.NewInput().Title(spec.Label).Value(&value)
	if spec.Help != "" {
		input = input.Description(spec.Help)
	}
	if spec.Default == "" && !spec.Required {
		input = input.Placeholder("optional")
	}
	input = input.Validate(func(s string) error {
		s = strings.TrimSpace(s)
		if s == "" {
			if spec.Required && spec.Default == "" {
				return errors.New("required")
			}
			return nil
		}
		if spec.Validate != nil {
			return spec.Validate(s)
		}
		return nil
	})

	if err := p.run(input); err != nil {
		return "", err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		value = spec.Default
	}
	return value, nil
}

// Password reads a secret without echoing it. It requires a terminal — off a
// TTY it errors rather than silently reading an echoed line, so callers must
// supply the secret another way (a flag, an env var). Rich terminals get a huh
// masked field; plain TTYs read via the terminal's no-echo mode.
func (p *Prompter) Password(label, help string) (string, error) {
	if !p.tty {
		return "", errors.New("cannot prompt for a password without a terminal; pass --password or set HTMLUP_PASSWORD")
	}
	if p.rich {
		value := ""
		input := huh.NewInput().Title(label).EchoMode(huh.EchoModePassword).Value(&value)
		if help != "" {
			input = input.Description(help)
		}
		input = input.Validate(func(s string) error {
			if strings.TrimSpace(s) == "" {
				return errors.New("required")
			}
			return nil
		})
		if err := p.run(input); err != nil {
			return "", err
		}
		return strings.TrimSpace(value), nil
	}

	f, ok := p.in.(*os.File)
	if !ok {
		return "", errors.New("cannot read password from this input")
	}
	if help != "" {
		fprintf(p.out, "\n  %s\n", help)
	}
	fprintf(p.out, "%s: ", label)
	raw, err := term.ReadPassword(int(f.Fd()))
	fprintln(p.out, "")
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(raw))
	if value == "" {
		return "", errors.New("required")
	}
	return value, nil
}

// Select picks one option from a fixed list. A single option is auto-selected
// and echoed. Otherwise huh renders an arrow-key list (rich) or a numbered
// menu (plain) pre-pointed at def.
func (p *Prompter) Select(label string, options []string, def string) (string, error) {
	if len(options) == 1 {
		fprintf(p.out, "%s: %s\n", label, options[0])
		return options[0], nil
	}
	if !p.rich {
		return p.selectPlain(label, options, def)
	}

	value := def
	opts := make([]huh.Option[string], len(options))
	for i, o := range options {
		opts[i] = huh.NewOption(o, o)
	}
	sel := huh.NewSelect[string]().Title(label).Options(opts...).Value(&value)
	if err := p.run(sel); err != nil {
		return "", err
	}
	return value, nil
}

// Confirm asks a yes/no question, returning def on empty input.
func (p *Prompter) Confirm(question string, def bool) (bool, error) {
	if !p.rich {
		return p.confirmPlain(question, def)
	}
	value := def
	c := huh.NewConfirm().Title(question).Value(&value)
	if err := p.run(c); err != nil {
		return false, err
	}
	return value, nil
}

// run executes a single-field huh form bound to this Prompter's streams and
// theme. Rendering to p.out (stderr for commands) keeps stdout machine-clean.
func (p *Prompter) run(field huh.Field) error {
	err := huh.NewForm(huh.NewGroup(field)).
		WithInput(p.in).
		WithOutput(p.out).
		WithTheme(p.theme).
		Run()
	if errors.Is(err, huh.ErrUserAborted) {
		return ErrAborted
	}
	return err
}

// --- plain fallback (non-TTY, NO_COLOR, tests) ---

func (p *Prompter) linePlain(spec LineSpec) (string, error) {
	for {
		if spec.Help != "" {
			fprintf(p.out, "\n  %s\n", spec.Help)
		}
		label := spec.Label
		switch {
		case spec.Default != "":
			label = fmt.Sprintf("%s [%s]", spec.Label, spec.Default)
		case !spec.Required:
			label += " (optional)"
		}
		fprintf(p.out, "%s: ", label)

		raw, err := p.r.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		v := strings.TrimSpace(raw)
		if v == "" {
			v = spec.Default
		}
		if v == "" && spec.Required {
			fprintln(p.out, "  (required)")
			if errors.Is(err, io.EOF) {
				return "", errors.New("required field; no input")
			}
			continue
		}
		if v != "" && spec.Validate != nil {
			if verr := spec.Validate(v); verr != nil {
				fprintf(p.out, "  invalid: %v\n", verr)
				if errors.Is(err, io.EOF) {
					return "", verr
				}
				continue
			}
		}
		return v, nil
	}
}

func (p *Prompter) selectPlain(label string, options []string, def string) (string, error) {
	for {
		fprintf(p.out, "%s:\n", label)
		for i, o := range options {
			marker := "  "
			if o == def {
				marker = "* "
			}
			fprintf(p.out, "  %s%d) %s\n", marker, i+1, o)
		}
		prompt := "choose"
		if def != "" {
			prompt = fmt.Sprintf("choose [%s]", def)
		}
		fprintf(p.out, "%s: ", prompt)

		raw, err := p.r.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		v := strings.TrimSpace(raw)
		if v == "" {
			if def == "" {
				fprintln(p.out, "  (required)")
				if errors.Is(err, io.EOF) {
					return "", errors.New("no selection")
				}
				continue
			}
			return def, nil
		}
		if n, perr := strconv.Atoi(v); perr == nil && n >= 1 && n <= len(options) {
			return options[n-1], nil
		}
		for _, o := range options {
			if o == v {
				return o, nil
			}
		}
		fprintf(p.out, "  not a valid choice: %q\n", v)
		if errors.Is(err, io.EOF) {
			return "", fmt.Errorf("not a valid choice: %q", v)
		}
	}
}

func (p *Prompter) confirmPlain(question string, def bool) (bool, error) {
	suffix := " [y/N]"
	if def {
		suffix = " [Y/n]"
	}
	for {
		fprintf(p.out, "%s%s: ", question, suffix)
		raw, err := p.r.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return false, err
		}
		v := strings.ToLower(strings.TrimSpace(raw))
		if v == "" {
			return def, nil
		}
		switch v {
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		}
		fprintln(p.out, "  please answer y or n")
		if errors.Is(err, io.EOF) {
			return def, nil
		}
	}
}
