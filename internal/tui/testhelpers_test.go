package tui

import (
	"strings"

	"github.com/ZenNotes/tui/internal/keymaps"
)

func newTestResolver() *keymaps.Resolver { return keymaps.NewResolver(nil) }

func contains(s, sub string) bool { return strings.Contains(s, sub) }
