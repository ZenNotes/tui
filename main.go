// zn: the standalone ZenNotes command-line interface and terminal UI.
//
// The binary mirrors the `zn` CLI the desktop app bundles (same commands,
// flags, output and JSON shapes) without needing the app installed, and adds
// `zn tui`, a keyboard-first terminal version of ZenNotes with full Vim
// motions. Notes stay plain Markdown files on disk; a vault behind a
// self-hosted ZenNotes server is reached over its HTTP API.
package main

import (
	"os"

	"github.com/ZenNotes/zennotescli/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:]))
}
