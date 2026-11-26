// Package i18n provides UI translations with a readable English fallback.
package i18n

import (
	"cmp"
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

//go:embed locales/*.json
var files embed.FS
var catalogs = load()

func load() map[string]map[string]string {
	out := map[string]map[string]string{}
	for _, lang := range []string{"en", "zh-CN"} {
		data, err := files.ReadFile("locales/" + lang + ".json")
		if err != nil {
			panic(err)
		}
		var catalog map[string]string
		if err = json.Unmarshal(data, &catalog); err != nil {
			panic(err)
		}
		out[lang] = catalog
	}
	return out
}
func Resolve(choice string) string {
	if choice == "" || choice == "auto" {
		choice = cmp.Or(os.Getenv("LC_ALL"), os.Getenv("LC_MESSAGES"), os.Getenv("LANG"))
		switch localeBase(choice) {
		case "c", "posix":
			return "en"
		}
		if list := os.Getenv("LANGUAGE"); list != "" {
			for preference := range strings.SplitSeq(list, ":") {
				switch localeBase(preference) {
				case "zh":
					return "zh-CN"
				case "en", "c", "posix":
					return "en"
				}
			}
			return "en"
		}
	}
	if localeBase(choice) == "zh" {
		return "zh-CN"
	}
	return "en"
}

func localeBase(tag string) string {
	tag = strings.TrimSpace(tag)
	if i := strings.IndexAny(tag, "-_.@"); i >= 0 {
		tag = tag[:i]
	}
	return strings.ToLower(tag)
}

// Count formats an inventory count for both the TUI and text snapshots.
func Count(value int, available bool) string {
	if !available {
		return "—"
	}
	return strconv.Itoa(value)
}

func Text(language, key string, args ...any) string {
	value := catalogs[Resolve(language)][key]
	if value == "" {
		value = key
	}
	if len(args) > 0 {
		return fmt.Sprintf(value, args...)
	}
	return value
}
