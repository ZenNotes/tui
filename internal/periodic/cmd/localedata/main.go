// Command localedata prints the CLDR language tags used to generate date names.
package main

import (
	"encoding/json"
	"golang.org/x/text/language/display"
	"os"
)

func main() {
	tags := []string{}
	for _, t := range display.Supported.Tags() {
		tags = append(tags, t.String())
	}
	_ = json.NewEncoder(os.Stdout).Encode(tags)
}
