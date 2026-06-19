package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

const DefaultRelPath = ".htmlup/config.toml"

type Profile map[string]string

type Config struct {
	Default   string                        `toml:"default,omitempty"`
	Providers map[string]map[string]Profile `toml:"providers,omitempty"`
}

func Empty() Config {
	return Config{Providers: map[string]map[string]Profile{}}
}

func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, DefaultRelPath), nil
}

func Load(path string) (Config, error) {
	if path == "" {
		var err error
		path, err = DefaultPath()
		if err != nil {
			return Config{}, err
		}
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Empty(), nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("reading config: %w", err)
	}
	cfg, err := Parse(string(data))
	if err != nil {
		return Config{}, fmt.Errorf("parsing config: %w", err)
	}
	return cfg, nil
}

func Save(path string, cfg Config) error {
	if path == "" {
		var err error
		path, err = DefaultPath()
		if err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}
	data, err := toml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	return nil
}

func Parse(input string) (Config, error) {
	cfg := Empty()
	if err := toml.Unmarshal([]byte(input), &cfg); err != nil {
		return Config{}, err
	}
	if cfg.Providers == nil {
		cfg.Providers = map[string]map[string]Profile{}
	}
	return cfg, nil
}

func (cfg Config) TOML() string {
	// ponytail: only `config inspect` reads this; a struct of strings never fails to marshal.
	data, _ := toml.Marshal(cfg)
	return string(data)
}

func (cfg Config) DefaultProviderProfile() (provider, profile string, ok bool) {
	provider, profile, ok = SplitRef(cfg.Default)
	return provider, profile, ok
}

func (cfg Config) PublishDefault() (providerName, profileName string, profile Profile, ok bool) {
	defaultProvider, defaultProfile, defaultOK := cfg.DefaultProviderProfile()
	if defaultOK {
		if selected, exists := cfg.Profile(defaultProvider, defaultProfile); exists {
			return defaultProvider, defaultProfile, selected, true
		}
	}

	var foundProvider, foundProfile string
	var found Profile
	count := 0
	for p, profiles := range cfg.Providers {
		for name, profile := range profiles {
			foundProvider, foundProfile, found = p, name, profile
			count++
			if count > 1 {
				return "", "", nil, false
			}
		}
	}
	if count == 1 {
		return foundProvider, foundProfile, found, true
	}
	return "", "", nil, false
}

func (cfg Config) Profile(providerName, profileName string) (Profile, bool) {
	profiles := cfg.Providers[providerName]
	if profiles == nil {
		return nil, false
	}
	profile, ok := profiles[profileName]
	return profile, ok
}

func (cfg Config) ProviderDefault(providerName, profileName string) (Profile, string, error) {
	if profileName != "" {
		profile, ok := cfg.Profile(providerName, profileName)
		if !ok {
			return nil, "", fmt.Errorf("config profile %q for provider %q not found", profileName, providerName)
		}
		return profile, profileName, nil
	}
	defaultProvider, defaultProfile, ok := cfg.DefaultProviderProfile()
	if ok && defaultProvider == providerName {
		profile, exists := cfg.Profile(providerName, defaultProfile)
		if exists {
			return profile, defaultProfile, nil
		}
	}
	profiles := cfg.Providers[providerName]
	if len(profiles) == 1 {
		for name, profile := range profiles {
			return profile, name, nil
		}
	}
	return Profile{}, "", nil
}

func Set(cfg Config, dottedKey, value string) (Config, error) {
	if cfg.Providers == nil {
		cfg.Providers = map[string]map[string]Profile{}
	}
	if dottedKey == "default" {
		if value != "" {
			if _, _, ok := SplitRef(value); !ok {
				return Config{}, fmt.Errorf("default must be in provider.profile format")
			}
		}
		cfg.Default = value
		return cfg, nil
	}
	parts := strings.Split(dottedKey, ".")
	if len(parts) != 4 || parts[0] != "providers" || parts[1] == "" || parts[2] == "" || parts[3] == "" {
		return Config{}, fmt.Errorf("key must be default or providers.<provider>.<profile>.<field>")
	}
	if !validName(parts[1]) || !validName(parts[2]) || !validName(parts[3]) {
		return Config{}, fmt.Errorf("provider, profile, and field names may only contain letters, numbers, underscores, and hyphens")
	}
	ensureProfile(cfg, parts[1], parts[2])
	cfg.Providers[parts[1]][parts[2]][parts[3]] = value
	return cfg, nil
}

func SplitRef(ref string) (provider, profile string, ok bool) {
	provider, profile, ok = strings.Cut(ref, ".")
	if !ok || !validName(provider) || !validName(profile) || strings.Contains(profile, ".") {
		return "", "", false
	}
	return provider, profile, true
}

// ValidName reports whether s is a legal provider/profile/field identifier
// (letters, digits, underscores, hyphens; non-empty).
func ValidName(s string) bool { return validName(s) }

func validName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func ensureProfile(cfg Config, providerName, profileName string) {
	if cfg.Providers == nil {
		cfg.Providers = map[string]map[string]Profile{}
	}
	if cfg.Providers[providerName] == nil {
		cfg.Providers[providerName] = map[string]Profile{}
	}
	if cfg.Providers[providerName][profileName] == nil {
		cfg.Providers[providerName][profileName] = Profile{}
	}
}
