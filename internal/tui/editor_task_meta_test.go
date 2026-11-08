package tui

import (
	"strings"
	"testing"
)

// metaSpans returns the text of every run of the line that lineClasses styles
// as task metadata.
func metaSpans(line string) []string {
	classes := lineClasses(line, false, false)
	runes := []rune(line)
	var out []string
	start := -1
	for i := 0; i <= len(runes); i++ {
		in := i < len(runes) && classes[i] == stMeta
		switch {
		case in && start < 0:
			start = i
		case !in && start >= 0:
			out = append(out, string(runes[start:i]))
			start = -1
		}
	}
	return out
}

func TestEditorStylesRepeatAndSkipTokens(t *testing.T) {
	got := metaSpans("- [ ] Water plants @repeat:2w-mon/thu skip:2027-03-13..2027-03-21,2027-04-01")
	want := []string{"@repeat:2w-mon/thu", "skip:2027-03-13..2027-03-21,2027-04-01"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("styled %q, want %q", got, want)
	}
	got = metaSpans("- [ ] Trim @Repeat:monthly-Start SKIP:2027-03-13")
	want = []string{"@Repeat:monthly-Start", "SKIP:2027-03-13"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("keys must match in any case: styled %q, want %q", got, want)
	}
}

func TestEditorStylesWholeRangedDueToken(t *testing.T) {
	got := metaSpans("- [ ] Call due:2026-11-03T14:00-15:30")
	want := []string{"due:2026-11-03T14:00-15:30"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("styled %q, want %q", got, want)
	}
}

func TestEditorStylesWholeTimedDueToken(t *testing.T) {
	got := metaSpans("- [ ] Call due:2026-11-03T14:00 scheduled:2026-11-02T09:30")
	want := []string{"due:2026-11-03T14:00", "scheduled:2026-11-02T09:30"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("styled %q, want %q", got, want)
	}
}
