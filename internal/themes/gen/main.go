// Command gen rebuilds internal/themes/builtin.css from the desktop app's
// stylesheet. Run it from the repository root when the desktop's built-in
// themes change (and update the registry in registry.go to match
// lib/themes.ts):
//
//	go run ./internal/themes/gen /path/to/zennotes/packages/app-core/src/styles/index.css
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ZenNotes/tui/internal/themes"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: go run ./internal/themes/gen <desktop index.css>")
		os.Exit(2)
	}
	css, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	out, err := themes.FlattenBuiltin(string(css), "ZenNotes/zennotes packages/app-core/src/styles/index.css")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	target := filepath.Join("internal", "themes", "builtin.css")
	if err := os.WriteFile(target, []byte(out), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("wrote", target)
}
