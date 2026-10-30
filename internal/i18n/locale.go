// Package i18n provides UI translations with a readable English fallback.
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
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
	if choice != "" && choice != "auto" {
		return normalize(choice)
	}
	for _, key := range []string{"LC_ALL", "LANGUAGE", "LC_MESSAGES", "LANG"} {
		if v := os.Getenv(key); v != "" {
			return normalize(strings.Split(v, ":")[0])
		}
	}
	return "en"
}
func normalize(s string) string {
	if strings.HasPrefix(strings.ToLower(s), "zh") {
		return "zh-CN"
	}
	return "en"
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
