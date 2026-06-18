package github

import "testing"

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		p       *Provider
		wantErr bool
	}{
		{"valid", &Provider{repo: "owner/name", branch: "gh-pages"}, false},
		{"repo empty", &Provider{repo: "", branch: "gh-pages"}, true},
		{"repo no slash", &Provider{repo: "ownername", branch: "gh-pages"}, true},
		{"repo empty owner", &Provider{repo: "/name", branch: "gh-pages"}, true},
		{"repo empty name", &Provider{repo: "owner/", branch: "gh-pages"}, true},
		{"repo too many slashes", &Provider{repo: "a/b/c", branch: "gh-pages"}, false},
		{"branch empty", &Provider{repo: "owner/name"}, true},
		{"branch with slash", &Provider{repo: "owner/name", branch: "feature/demo"}, false},
		{"branch starts with slash", &Provider{repo: "owner/name", branch: "/demo"}, true},
		{"branch starts with dot", &Provider{repo: "owner/name", branch: ".hidden"}, true},
		{"branch component starts with dot", &Provider{repo: "owner/name", branch: "feature/.hidden"}, true},
		{"branch has parent segment", &Provider{repo: "owner/name", branch: "feature..demo"}, true},
		{"branch has lock suffix", &Provider{repo: "owner/name", branch: "feature/demo.lock"}, true},
		{"branch has reserved char", &Provider{repo: "owner/name", branch: "feature:demo"}, true},
		{"dir empty", &Provider{repo: "owner/name", branch: "gh-pages"}, false},
		{"dir clean nested", &Provider{repo: "owner/name", branch: "gh-pages", dir: "docs/reports"}, false},
		{"dir absolute", &Provider{repo: "owner/name", branch: "gh-pages", dir: "/docs"}, true},
		{"dir parent", &Provider{repo: "owner/name", branch: "gh-pages", dir: "../docs"}, true},
		{"dir not clean", &Provider{repo: "owner/name", branch: "gh-pages", dir: "docs/../site"}, true},
		{"dir backslash", &Provider{repo: "owner/name", branch: "gh-pages", dir: `docs\site`}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.p.validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
