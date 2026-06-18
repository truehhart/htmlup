// Package wizard drives `htmlup config init` — schema-driven interactive
// prompts that feed values through the same config.Set codepath the
// non-interactive `config set` command uses.
package wizard

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/truehhart/htmlup/internal/config"
	"github.com/truehhart/htmlup/internal/provider"
)

// Options controls one run of the wizard. Zero values mean "ask interactively"
// for everything except Stdin/Stdout which fall back to os.Stdin / os.Stdout.
type Options struct {
	Path           string            // config file path; empty -> config.DefaultPath
	ProviderName   string            // pre-selected provider; empty -> prompt
	ProfileName    string            // pre-selected profile name; empty -> prompt
	Preset         map[string]string // key=value pairs from --set, bypassing prompts
	NonInteractive bool              // refuse to prompt; missing required input is an error
	Force          bool              // overwrite an existing profile without confirming
	SetDefault     *bool             // explicit override for "make this the default?" prompt
	Stdin          io.Reader
	Stdout         io.Writer
}

// Result is the human-readable outcome of a successful wizard run.
type Result struct {
	Path     string // resolved on-disk config path
	Provider string
	Profile  string
	IsNew    bool // true when the profile didn't already exist
	Default  bool // true when this profile was set as the new default
}

// Run executes the wizard end-to-end: prompts (or applies presets), validates,
// and writes the updated config to disk. The returned Result describes what
// was written so the caller can print a friendly summary.
func Run(opts Options) (Result, error) {
	in := opts.Stdin
	if in == nil {
		in = os.Stdin
	}
	out := opts.Stdout
	if out == nil {
		out = os.Stdout
	}
	r := bufio.NewReader(in)

	cfg, err := config.Load(opts.Path)
	if err != nil {
		return Result{}, err
	}

	providerName, schema, err := resolveProvider(r, out, opts)
	if err != nil {
		return Result{}, err
	}

	profileName, err := resolveProfileName(r, out, opts)
	if err != nil {
		return Result{}, err
	}

	existing, exists := cfg.Profile(providerName, profileName)
	if exists && !opts.Force {
		if opts.NonInteractive {
			return Result{}, fmt.Errorf("profile %s.%s already exists (re-run with --force to overwrite)", providerName, profileName)
		}
		ok, err := promptConfirm(r, out, fmt.Sprintf("Profile %s.%s exists. Update it?", providerName, profileName), true)
		if err != nil {
			return Result{}, err
		}
		if !ok {
			return Result{}, errors.New("aborted")
		}
	}

	values, err := collectValues(r, out, schema, existing, opts)
	if err != nil {
		return Result{}, err
	}

	for k, v := range values {
		key := fmt.Sprintf("providers.%s.%s.%s", providerName, profileName, k)
		cfg, err = config.Set(cfg, key, v)
		if err != nil {
			return Result{}, err
		}
	}

	ref := providerName + "." + profileName
	setDefault, err := resolveDefault(r, out, cfg, ref, opts)
	if err != nil {
		return Result{}, err
	}
	if setDefault {
		cfg, err = config.Set(cfg, "default", ref)
		if err != nil {
			return Result{}, err
		}
	}

	if err := config.Save(opts.Path, cfg); err != nil {
		return Result{}, err
	}

	path := opts.Path
	if path == "" {
		path, _ = config.DefaultPath()
	}
	return Result{
		Path:     path,
		Provider: providerName,
		Profile:  profileName,
		IsNew:    !exists,
		Default:  setDefault,
	}, nil
}

func resolveProvider(r *bufio.Reader, out io.Writer, opts Options) (string, []provider.ConfigField, error) {
	names := provider.Names()
	if len(names) == 0 {
		return "", nil, errors.New("no providers registered")
	}

	name := opts.ProviderName
	if name == "" {
		if opts.NonInteractive {
			return "", nil, errors.New("--provider is required in non-interactive mode")
		}
		var err error
		name, err = promptSelect(r, out, "Provider", names, names[0])
		if err != nil {
			return "", nil, err
		}
	}

	p, ok := provider.Get(name)
	if !ok {
		return "", nil, fmt.Errorf("unknown provider %q (known: %s)", name, strings.Join(names, ", "))
	}
	schemaProvider, ok := p.(provider.ConfigSchemaProvider)
	if !ok {
		return "", nil, fmt.Errorf("provider %q does not declare a config schema yet", name)
	}
	return name, schemaProvider.ConfigSchema(), nil
}

func resolveProfileName(r *bufio.Reader, out io.Writer, opts Options) (string, error) {
	name := opts.ProfileName
	if name == "" {
		if opts.NonInteractive {
			return "default", nil
		}
		v, err := promptLine(r, out, promptSpec{
			Label:    "Profile name",
			Help:     "Used to refer to this configuration. 'default' is fine for most setups.",
			Default:  "default",
			Required: true,
			Validate: func(v string) error {
				if !config.ValidName(v) {
					return errors.New("only letters, numbers, underscores, and hyphens")
				}
				return nil
			},
		})
		if err != nil {
			return "", err
		}
		name = v
	}
	if !config.ValidName(name) {
		return "", fmt.Errorf("invalid profile name %q (letters, numbers, underscores, hyphens)", name)
	}
	return name, nil
}

func collectValues(r *bufio.Reader, out io.Writer, schema []provider.ConfigField, existing config.Profile, opts Options) (map[string]string, error) {
	// Reject typos like --set prfix=... up front: every preset key must match a
	// schema field, otherwise the value is silently dropped and the user ends up
	// with a half-configured profile.
	if len(opts.Preset) > 0 {
		known := make(map[string]struct{}, len(schema))
		for _, f := range schema {
			known[f.Key] = struct{}{}
		}
		var unknown []string
		for k := range opts.Preset {
			if _, ok := known[k]; !ok {
				unknown = append(unknown, k)
			}
		}
		if len(unknown) > 0 {
			sort.Strings(unknown)
			fields := make([]string, len(schema))
			for i, f := range schema {
				fields[i] = f.Key
			}
			return nil, fmt.Errorf("--set: unknown field(s) %s (known: %s)", strings.Join(unknown, ", "), strings.Join(fields, ", "))
		}
	}

	values := map[string]string{}
	for _, f := range schema {
		if preset, ok := opts.Preset[f.Key]; ok {
			if f.Required && preset == "" {
				return nil, fmt.Errorf("--set %s=\"\": required field", f.Key)
			}
			if preset != "" && f.Validate != nil {
				if err := f.Validate(preset); err != nil {
					return nil, fmt.Errorf("--set %s=%q: %w", f.Key, preset, err)
				}
			}
			if preset != "" {
				values[f.Key] = preset
			}
			continue
		}

		def := ""
		if existing != nil {
			def = existing[f.Key]
		}
		if def == "" && f.Default != nil {
			def = f.Default()
		}

		if opts.NonInteractive {
			if f.Required && def == "" {
				return nil, fmt.Errorf("missing required field %q (provide --set %s=...)", f.Key, f.Key)
			}
			if def != "" {
				values[f.Key] = def
			}
			continue
		}

		v, err := promptLine(r, out, promptSpec{
			Label:    f.Label,
			Help:     f.Help,
			Default:  def,
			Required: f.Required,
			Validate: f.Validate,
		})
		if err != nil {
			return nil, err
		}
		if v != "" {
			values[f.Key] = v
		}
	}
	return values, nil
}

func resolveDefault(r *bufio.Reader, out io.Writer, cfg config.Config, ref string, opts Options) (bool, error) {
	// Explicit --set-default wins over every implicit rule, including the
	// "no existing default → adopt this one" autopromotion below.
	if opts.SetDefault != nil {
		return *opts.SetDefault, nil
	}
	if cfg.Default == "" {
		return true, nil
	}
	if cfg.Default == ref {
		return false, nil
	}
	if opts.NonInteractive {
		return false, nil
	}
	return promptConfirm(r, out, fmt.Sprintf("Set %s as the default? (current default: %s)", ref, cfg.Default), false)
}
