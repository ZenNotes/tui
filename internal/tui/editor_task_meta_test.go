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

func TestEditorStylesRepeatSkipAndUntilFields(t *testing.T) {
	got := metaSpans("- [ ] Water plants @repeat:2w-mon/thu @skip:2027-03-13/2027-03-21 @until:2027-12-01")
	want := []string{"@repeat:2w-mon/thu", "@skip:2027-03-13/2027-03-21", "@until:2027-12-01"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("styled %q, want %q", got, want)
	}
	got = metaSpans("- [ ] Trim @Repeat:monthly-Start @SKIP:Spring-Break")
	want = []string{"@Repeat:monthly-Start", "@SKIP:Spring-Break"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("styled %q, want %q", got, want)
	}
}

func TestEditorLeavesBareTokensAndProseWhenPlain(t *testing.T) {
	for _, line := range []string{
		"- [ ] Water plants repeat:weekly skip:2027-03-13 until:2027-12-01",
		"- ask them when: tomorrow",
		"- the meeting is when:soon",
		"- the day when:2026-11-03x",
		"- see `when:2026-11-03`",
		"- a link task:Work",
	} {
		if got := metaSpans(line); len(got) != 0 {
			t.Errorf("%q styled %q, want nothing", line, got)
		}
	}
}

func TestEditorStylesEventTokens(t *testing.T) {
	cases := map[string][]string{
		"- Lecture when:2026-11-03T14:00-15:30 @repeat:weekly @until:2026-12-14": {"when:2026-11-03T14:00-15:30", "@repeat:weekly", "@until:2026-12-14"},
		"- Break when:2027-03-13/2027-03-21 @skip:Spring-Break":                  {"when:2027-03-13/2027-03-21", "@skip:Spring-Break"},
		"- Day when:2026-11-03": {"when:2026-11-03"},
		"- Work on it when:2026-11-03T09:00 task:[[Work#^ab12cd]] ^ef34gh": {"when:2026-11-03T09:00", "task:[[Work#^ab12cd]]"},
		"- Plan when:2026-11-03T09:00 task:[[My note]]":                    {"when:2026-11-03T09:00", "task:[[My note]]"},
	}
	for line, want := range cases {
		if got := metaSpans(line); strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("%q styled %q, want %q", line, got, want)
		}
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
