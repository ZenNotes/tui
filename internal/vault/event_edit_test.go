package vault

import "testing"

func TestSetEventWhenInline(t *testing.T) {
	body := "# Plans\r\n- Gym when:2026-11-04T07:00\r\n- Dentist `when:x` when:2026-11-05T14:00-15:30 ^id1\r\n"
	events := ParseEvents("Plans.md", "Plans", FolderInbox, body)
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	got, ok := SetEventWhen(body, events[1], "2026-11-02T14:00-15:30")
	want := "# Plans\r\n- Gym when:2026-11-04T07:00\r\n- Dentist `when:x` when:2026-11-02T14:00-15:30 ^id1\r\n"
	if !ok || got != want {
		t.Errorf("SetEventWhen = %q, %v; want %q", got, ok, want)
	}
	moved := events[1]
	moved.When = "2026-11-06"
	if got, ok := SetEventWhen(body, moved, "2026-11-02"); ok || got != body {
		t.Errorf("a stale event changed the body: %q, %v", got, ok)
	}
}

func TestDeleteEventLine(t *testing.T) {
	body := "- Gym when:2026-11-04T07:00\n- Dentist when:2026-11-05\n- Walk when:2026-11-09"
	events := ParseEvents("Plans.md", "Plans", FolderInbox, body)
	got, ok := DeleteEventLine(body, events[2])
	if want := "- Gym when:2026-11-04T07:00\n- Dentist when:2026-11-05"; !ok || got != want {
		t.Errorf("DeleteEventLine = %q, %v; want %q", got, ok, want)
	}
	got, ok = DeleteEventLine(body, events[0])
	if want := "- Dentist when:2026-11-05\n- Walk when:2026-11-09"; !ok || got != want {
		t.Errorf("DeleteEventLine = %q, %v; want %q", got, ok, want)
	}
}

func TestSetEventWhenNoteKeepsQuotes(t *testing.T) {
	for _, c := range []struct{ old, want string }{
		{"when: 2026-11-03T14:00", "when: 2026-11-02T14:00"},
		{`when: "2026-11-03T14:00"`, `when: "2026-11-02T14:00"`},
		{"when:2026-11-03T14:00", "when:2026-11-02T14:00"},
	} {
		body := "---\npage-type: event\n" + c.old + "\ntags: [a]\n---\nBody when: 2026-11-03\n"
		events := ParseEvents("Dentist.md", "Dentist", FolderInbox, body)
		if len(events) != 1 {
			t.Fatalf("%q: events = %d", c.old, len(events))
		}
		got, ok := SetEventWhen(body, events[0], "2026-11-02T14:00")
		want := "---\npage-type: event\n" + c.want + "\ntags: [a]\n---\nBody when: 2026-11-03\n"
		if !ok || got != want {
			t.Errorf("%q: got %q, %v", c.old, got, ok)
		}
	}
}
