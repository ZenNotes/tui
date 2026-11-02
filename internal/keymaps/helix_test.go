package keymaps

import "testing"

// Earlier helix-branch builds wrote helix.* ids for the shared modal
// actions; they still apply, and the vim.* id wins when both are set.
func TestHelixIDsAliasVimIDs(t *testing.T) {
	r := NewResolver(map[string]string{"helix.leaderPrefix": ",", "helix.paneFocusLeft": "a", "vim.paneFocusLeft": "b", "helix.gotoTabNext": "g x"})
	if got := r.Binding("vim.leaderPrefix"); got != "," {
		t.Errorf("helix.leaderPrefix override not applied: %q", got)
	}
	if got := r.Binding("vim.paneFocusLeft"); got != "b" {
		t.Errorf("vim.* override should win over helix.*: %q", got)
	}
	if got := r.Binding("helix.gotoTabNext"); got != "g x" {
		t.Errorf("helix-only id override not applied: %q", got)
	}
}
