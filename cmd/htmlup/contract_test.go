package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// User-facing output must go through internal/ui — both the byte streams it
// writes to (os.Stdout / os.Stderr) and the rendering libraries behind it
// (charmbracelet/*). This test walks the AST of every non-test .go file
// outside the allowed directories and complains if any of those primitives
// leak out.

// allowedDirs may use the otherwise-forbidden primitives: internal/ui owns
// them. Add directories here only with a documented reason.
var allowedDirs = []string{
	filepath.FromSlash("internal/ui"),
}

// forbiddenImports flags imports that route around internal/ui.
var forbiddenImports = []struct {
	prefix string
	use    string
}{
	{"github.com/charmbracelet/", "the internal/ui surface (extend ui's wrappers instead)"},
}

// forbiddenSelectors flags references that would route around ui. os.Stdout /
// os.Stderr catches the direct-stream cases (including fmt.Fprintf(os.Stderr,
// …)); fmt.Print* catches the implicit-stdout shorthand fmt offers. We do not
// flag fmt.Fprint* by name because it's a generic writer helper — innocuous
// when its target is, say, a strings.Builder; harmful only when its target is
// os.Std*, which the previous rule already covers.
type selectorRule struct {
	pkg      string          // qualifier on the left of the dot ("os", "fmt", "cmd")
	names    map[string]bool // exact identifier names after the dot
	prefixes []string        // alternative match: identifier starts with any of these
	use      string
}

var forbiddenSelectors = []selectorRule{
	{
		pkg:   "os",
		names: map[string]bool{"Stdout": true, "Stderr": true},
		use:   "ui.Output (Result/Plain or Info/Success/Warn/…)",
	},
	{
		pkg:      "fmt",
		prefixes: []string{"Print"},
		use:      "ui.Output (fmt.Print/Printf/Println write to os.Stdout)",
	},
	{
		pkg:      "cmd",
		prefixes: []string{"Print"},
		use:      "ui.Output (cobra's cmd.Print* land in cobra's output stream, not ui)",
	},
}

func TestNoDirectUserFacingOutput(t *testing.T) {
	root := repoRoot(t)
	fset := token.NewFileSet()

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if isAllowed(rel) {
			return nil
		}

		f, parseErr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return parseErr
		}
		for _, imp := range f.Imports {
			ipath := strings.Trim(imp.Path.Value, `"`)
			for _, rule := range forbiddenImports {
				if strings.HasPrefix(ipath, rule.prefix) {
					t.Errorf("%s imports %q — route user-facing output through %s", rel, ipath, rule.use)
				}
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			x, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			for _, rule := range forbiddenSelectors {
				if x.Name != rule.pkg {
					continue
				}
				if rule.names != nil && rule.names[sel.Sel.Name] {
					t.Errorf("%s references %s.%s — use %s instead", rel, x.Name, sel.Sel.Name, rule.use)
				}
				for _, p := range rule.prefixes {
					if strings.HasPrefix(sel.Sel.Name, p) {
						t.Errorf("%s calls %s.%s — use %s instead", rel, x.Name, sel.Sel.Name, rule.use)
					}
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walking source tree: %v", err)
	}
}

func isAllowed(rel string) bool {
	for _, dir := range allowedDirs {
		if rel == dir || strings.HasPrefix(rel, dir+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// repoRoot walks up from this package directory until it finds go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate go.mod above the test package")
		}
		dir = parent
	}
}
