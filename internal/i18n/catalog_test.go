package i18n_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/souProjet/trade-republic-exporter/internal/app"
	"github.com/souProjet/trade-republic-exporter/internal/config"
	"github.com/souProjet/trade-republic-exporter/internal/export"
	"github.com/souProjet/trade-republic-exporter/internal/i18n"
)

// messages collects every literal passed to i18n.T or i18n.N in the source
// tree, plus
// the messages built from data: dataset labels, setting descriptions, phases.
func messages(t *testing.T) map[string]string {
	t.Helper()
	found := map[string]string{}
	fset := token.NewFileSet()
	for _, root := range []string{"../../internal", "../../cmd"} {
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			file, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return err
			}
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || len(call.Args) == 0 {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || (sel.Sel.Name != "T" && sel.Sel.Name != "N") {
					return true
				}
				if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "i18n" {
					return true
				}
				if lit, ok := call.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
					if msg, err := strconv.Unquote(lit.Value); err == nil {
						found[msg] = fset.Position(lit.Pos()).String()
					}
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, d := range export.Datasets {
		found[d.Label], found[d.Description] = "export.Datasets", "export.Datasets"
	}
	for _, k := range config.Keys {
		found[k.Description] = "config.Keys"
	}
	for _, s := range []config.Source{config.SourceDefault, config.SourceFile, config.SourceEnv, config.SourceKeychain} {
		found[string(s)] = "config.Source"
	}
	for _, p := range []string{app.PhaseAuthentication, app.PhaseSession, app.PhaseExport} {
		found[p] = "app phases"
	}
	for _, s := range []string{"Securities account", "PEA", "unknown"} {
		found[s] = "trws.Account.Label"
	}
	return found
}

func TestFrenchCatalogIsComplete(t *testing.T) {
	found := messages(t)
	i18n.Set(i18n.French)
	defer i18n.Set(i18n.English)

	var missing []string
	for msg, where := range found {
		if i18n.T(msg) == msg && !sameInFrench[msg] {
			missing = append(missing, where+": "+strconv.Quote(msg))
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("%d messages have no French translation:\n%s", len(missing), strings.Join(missing, "\n"))
	}
}

func TestFrenchKeepsFormatVerbs(t *testing.T) {
	i18n.Set(i18n.French)
	defer i18n.Set(i18n.English)
	for msg := range messages(t) {
		if got, want := verbs(i18n.T(msg)), verbs(msg); got != want {
			t.Errorf("%q: format verbs %q, want %q", msg, got, want)
		}
	}
}

// verbs lists the format verbs of a message, in order.
func verbs(s string) string {
	var out []string
	for i := 0; i < len(s)-1; i++ {
		if s[i] != '%' {
			continue
		}
		if s[i+1] == '%' {
			i++
			continue
		}
		j := i + 1
		for j < len(s) && strings.ContainsRune("0123456789.+-# ", rune(s[j])) {
			j++
		}
		if j < len(s) {
			out = append(out, s[i:j+1])
		}
		i = j
	}
	return strings.Join(out, " ")
}

// sameInFrench lists messages that read the same in both languages.
var sameInFrench = map[string]bool{
	"WebSocket":                 true,
	"PEA":                       true,
	"Format":                    true,
	"PIN":                       true,
	"Session":                   true,
	"Export":                    true,
	"Standard":                  true,
	"Interface":                 true,
	"Positions":                 true,
	"Transactions":              true,
	"Mode":                      true,
	"code":                      true,
	"env":                       true,
	"page %d · %d transactions": true,
}

func TestDetect(t *testing.T) {
	for _, tc := range []struct {
		env  map[string]string
		want i18n.Language
	}{
		{map[string]string{"LC_ALL": "fr_FR.UTF-8"}, i18n.French},
		{map[string]string{"LC_ALL": "", "LC_MESSAGES": "en_GB.UTF-8"}, i18n.English},
	} {
		for _, k := range []string{"LC_ALL", "LC_MESSAGES"} {
			t.Setenv(k, tc.env[k])
		}
		if got := i18n.Detect(); got != tc.want {
			t.Errorf("Detect with %v = %s, want %s", tc.env, got, tc.want)
		}
	}
}

func TestFallbackAndFormatting(t *testing.T) {
	i18n.Set(i18n.French)
	defer i18n.Set(i18n.English)
	if got := i18n.T("a message nobody translated %d", 3); got != "a message nobody translated 3" {
		t.Errorf("fallback = %q", got)
	}
	i18n.Set("de")
	if i18n.Current() != i18n.English {
		t.Errorf("an unknown language must fall back to English, got %s", i18n.Current())
	}
}
