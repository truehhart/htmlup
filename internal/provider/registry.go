package provider

import (
	"fmt"
	"sort"
)

var registry = map[string]Provider{}

func Register(p Provider) {
	name := p.Name()
	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("provider %q already registered", name))
	}
	registry[name] = p
}

func Get(name string) (Provider, bool) {
	p, ok := registry[name]
	return p, ok
}

func Names() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func All() []Provider {
	names := Names()
	providers := make([]Provider, len(names))
	for i, name := range names {
		providers[i] = registry[name]
	}
	return providers
}
