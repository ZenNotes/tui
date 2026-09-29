package tui

import (
	"encoding/json"
	"fmt"
	"github.com/ZenNotes/tui/internal/config"
	"os"
	"path/filepath"
	"time"
)

// Recovery copies are only needed when the process exits with failed/pending
// saves. They are separate from the vault so external versions stay untouched.
func (a *App) writeRecovery(cause error) error {
	notes := map[string]string{}
	for p, b := range a.buffers {
		if b.dirty() {
			notes[p] = b.ed.Text()
		}
	}
	if len(notes) == 0 {
		return cause
	}
	dir := filepath.Join(config.UserDataDir(), "tui-recovery")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("%v; recovery failed: %w", cause, err)
	}
	data, err := json.MarshalIndent(struct {
		Vault   string            `json:"vault"`
		SavedAt string            `json:"savedAt"`
		Notes   map[string]string `json:"notes"`
	}{a.sessionKey(), time.Now().Format(time.RFC3339), notes}, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "unsaved-*.json")
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return fmt.Errorf("%v; unsaved edits recovered to %s", cause, f.Name())
}
