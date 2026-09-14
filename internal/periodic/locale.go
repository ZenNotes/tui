package periodic

import (
	_ "embed"
	"encoding/json"
	"golang.org/x/text/language"
	"os"
	"sort"
	"strings"
)

// Names are generated with Intl.DateTimeFormat, as used by the desktop.
//
//go:embed date_names.json
var dateNamesJSON []byte
var dateNames map[string]map[string][]string
var dateLocales []string
var dateMatcher language.Matcher

func init() {
	if err := json.Unmarshal(dateNamesJSON, &dateNames); err != nil {
		panic(err)
	}
	for locale := range dateNames {
		dateLocales = append(dateLocales, locale)
	}
	sort.Strings(dateLocales)
	tags := make([]language.Tag, len(dateLocales))
	for i, s := range dateLocales {
		tags[i] = language.Make(s)
	}
	dateMatcher = language.NewMatcher(tags)
}
func namesForLocale(locale string) map[string][]string {
	if locale == "" || locale == "system" {
		for _, key := range []string{"LC_ALL", "LC_TIME", "LANG"} {
			if value := os.Getenv(key); value != "" {
				locale = value
				break
			}
		}
	}
	locale = strings.ReplaceAll(strings.Split(strings.Split(locale, ".")[0], "@")[0], "_", "-")
	if locale == "" || locale == "C" || locale == "POSIX" || locale == "system" {
		locale = "en"
	}
	_, i, confidence := dateMatcher.Match(language.Make(locale))
	if confidence == language.No {
		return dateNames["en"]
	}
	return dateNames[dateLocales[i]]
}
