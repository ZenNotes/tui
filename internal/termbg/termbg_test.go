package termbg_test

import (
	"fmt"
	"os"
	"testing"

	// Linked on purpose: its init is the terminal query this package
	// pre-empts, so the child processes below behave like zn does.
	_ "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ZenNotes/tui/internal/termbg"
)

// The test binary doubles as the child process the pty tests run: it must
// import Bubble Tea and termbg exactly like zn, and nothing else can.
func TestMain(m *testing.M) {
	switch os.Getenv("ZN_TERMBG_CHILD") {
	case "":
		os.Exit(m.Run())
	case "startup":
		fmt.Println("child-ok")
	case "detect":
		fmt.Println(map[bool]string{true: "dark", false: "light"}[termbg.Detect()])
	}
	os.Exit(0)
}

func TestGuessReadsCOLORFGBG(t *testing.T) {
	for input, dark := range map[string]bool{"": true, "15;0": true, "0;15": false, "0;7": false, "7;default;0": true, "junk": true, "0;99": true} {
		if got := termbg.Guess(input); got != dark {
			t.Errorf("Guess(%q) = %v, want %v", input, got, dark)
		}
	}
}

// After init, Lip Gloss has an explicit answer, so nothing that asks the
// default renderer queries the terminal.
func TestDefaultRendererNeedsNoQuery(t *testing.T) {
	if got := lipgloss.HasDarkBackground(); got != termbg.Guess(os.Getenv("COLORFGBG")) {
		t.Fatalf("default renderer reports %v, want the preset guess", got)
	}
}
