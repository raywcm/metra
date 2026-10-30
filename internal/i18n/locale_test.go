package i18n

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestLocaleDetectionAndFallback(t *testing.T) {
	for _, k := range []string{"LC_ALL", "LANGUAGE", "LC_MESSAGES", "LANG"} {
		t.Setenv(k, "")
	}
	t.Setenv("LANG", "zh_CN.UTF-8")
	if Resolve("auto") != "zh-CN" || Text("auto", "Memory") != "内存" {
		t.Fatal("Chinese locale not detected")
	}
	t.Setenv("LC_ALL", "C")
	if Resolve("auto") != "en" {
		t.Fatal("LC_ALL precedence")
	}
	if Resolve("zh-CN") != "zh-CN" || Resolve("de-DE") != "en" {
		t.Fatal("fallback")
	}
	if Text("zh-CN", "unknown key") != "unknown key" {
		t.Fatal("missing key fallback")
	}
}
func TestCatalogCompletenessAndFormatParity(t *testing.T) {
	en, zh := catalogs["en"], catalogs["zh-CN"]
	if len(en) < 100 || len(en) != len(zh) {
		t.Fatal("incomplete catalogs")
	}
	directive := regexp.MustCompile(`%[-+#0 .0-9]*[vTtbcdoOqxXUeEfFgGspw%]`)
	for key, text := range en {
		translated, ok := zh[key]
		if !ok || translated == "" {
			t.Fatalf("missing %q", key)
		}
		if strings.Join(directive.FindAllString(text, -1), ",") != strings.Join(directive.FindAllString(translated, -1), ",") {
			t.Fatalf("format mismatch: %q", key)
		}
	}
	for _, lang := range []string{"en", "zh-CN"} {
		data, err := files.ReadFile("locales/" + lang + ".json")
		if err != nil || !json.Valid(data) {
			t.Fatal(lang, err)
		}
	}
}
func TestEveryUIMessageHasTranslations(t *testing.T) {
	paths, err := filepath.Glob("../tui/*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, e := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if e != nil {
			t.Fatal(e)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "tr" {
				return true
			}
			arg, ok := call.Args[0].(*ast.BasicLit)
			if !ok {
				return true
			}
			key, e := strconv.Unquote(arg.Value)
			if e != nil {
				t.Fatal(e)
			}
			for _, lang := range []string{"en", "zh-CN"} {
				if catalogs[lang][key] == "" {
					t.Errorf("%s missing translation %q", path, key)
				}
			}
			return true
		})
	}
}
