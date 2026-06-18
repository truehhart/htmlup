package wizard

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// writef is a fmt.Fprintf that swallows the error. The wizard writes to
// stdout/stderr (or a test buffer); a write failure there isn't recoverable
// and would be reported elsewhere anyway.
func writef(w io.Writer, format string, a ...any) {
	_, _ = fmt.Fprintf(w, format, a...)
}

type promptSpec struct {
	Label    string
	Help     string
	Default  string
	Required bool
	Validate func(string) error
}

// promptLine reads a single line, applying default + validation. Empty input
// keeps the default; required fields with no default re-prompt.
func promptLine(r *bufio.Reader, out io.Writer, spec promptSpec) (string, error) {
	for {
		if spec.Help != "" {
			writef(out, "\n  %s\n", spec.Help)
		}
		label := spec.Label
		if spec.Default != "" {
			label = fmt.Sprintf("%s [%s]", spec.Label, spec.Default)
		} else if !spec.Required {
			label = spec.Label + " (optional)"
		}
		writef(out, "%s: ", label)

		raw, err := r.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		v := strings.TrimSpace(raw)
		if v == "" {
			v = spec.Default
		}
		if v == "" && spec.Required {
			writef(out, "%s\n", "  (required)")
			if errors.Is(err, io.EOF) {
				return "", errors.New("required field; no input")
			}
			continue
		}
		if v != "" && spec.Validate != nil {
			if verr := spec.Validate(v); verr != nil {
				writef(out, "  invalid: %v\n", verr)
				if errors.Is(err, io.EOF) {
					return "", verr
				}
				continue
			}
		}
		return v, nil
	}
}

// promptSelect picks one option from a fixed list. Accepts the index (1-based)
// or the exact value; empty input picks def.
func promptSelect(r *bufio.Reader, out io.Writer, label string, options []string, def string) (string, error) {
	if len(options) == 1 {
		writef(out, "%s: %s\n", label, options[0])
		return options[0], nil
	}
	for {
		writef(out, "%s:\n", label)
		for i, o := range options {
			marker := "  "
			if o == def {
				marker = "* "
			}
			writef(out, "  %s%d) %s\n", marker, i+1, o)
		}
		prompt := "choose"
		if def != "" {
			prompt = fmt.Sprintf("choose [%s]", def)
		}
		writef(out, "%s: ", prompt)

		raw, err := r.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		v := strings.TrimSpace(raw)
		if v == "" {
			if def == "" {
				writef(out, "%s\n", "  (required)")
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
		writef(out, "  not a valid choice: %q\n", v)
		if errors.Is(err, io.EOF) {
			return "", fmt.Errorf("not a valid choice: %q", v)
		}
	}
}

func promptConfirm(r *bufio.Reader, out io.Writer, question string, def bool) (bool, error) {
	suffix := " [y/N]"
	if def {
		suffix = " [Y/n]"
	}
	for {
		writef(out, "%s%s: ", question, suffix)
		raw, err := r.ReadString('\n')
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
		writef(out, "  please answer y or n\n")
		if errors.Is(err, io.EOF) {
			return def, nil
		}
	}
}
