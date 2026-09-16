package vault

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const noteMetadataDir = "note-metadata"
const noteMetadataSuffix = ".metadata.json"

type noteCreationMetadata struct {
	Version   int   `json:"version"`
	CreatedAt int64 `json:"createdAt"`
}

func (v *Vault) metadataPath(rel string, directory bool) (string, error) {
	if !directory {
		rel += noteMetadataSuffix
	}
	return SafeJoin(v.root, filepath.Join(InternalVaultDir, noteMetadataDir, rel))
}

func readCreationMetadata(abs string) (int64, error) {
	raw, err := os.ReadFile(abs)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var metadata noteCreationMetadata
	if len(raw) > 4096 || json.Unmarshal(raw, &metadata) != nil || metadata.Version != 1 || metadata.CreatedAt <= 0 || metadata.CreatedAt > 8640000000000000 {
		return 0, fmt.Errorf("invalid note creation metadata: %s", abs)
	}
	return metadata.CreatedAt, nil
}

func (v *Vault) noteCreatedAt(rel string, fallback int64) int64 {
	abs, err := v.metadataPath(rel, false)
	if err == nil {
		if created, err := readCreationMetadata(abs); err == nil && created > 0 {
			return created
		}
	}
	// A broken optional sidecar must not hide the note from a read-only client.
	return fallback
}

func (v *Vault) writeNoteFileAtomic(abs string, data []byte) error {
	rel := v.relPosix(abs)
	ext := strings.ToLower(filepath.Ext(abs))
	if strings.HasPrefix(rel, InternalVaultDir+"/") || (ext != ".md" && ext != ".excalidraw") {
		return writeFileAtomic(abs, data, v.fileMode, v.dirMode)
	}
	metadataPath, err := v.metadataPath(rel, false)
	if err != nil {
		return err
	}
	// Inspect the note before the sidecar. Another writer publishes its sidecar
	// before replacing the note, so a new inode cannot seed a newer date here.
	info, err := os.Stat(abs)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.Remove(metadataPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	} else if err != nil {
		return err
	} else {
		originalCreatedAt, _ := fileTimes(abs, info)
		created, err := readCreationMetadata(metadataPath)
		if err != nil {
			return err
		}
		if created == 0 {
			created = originalCreatedAt
			raw, err := json.Marshal(noteCreationMetadata{Version: 1, CreatedAt: created})
			if err != nil {
				return err
			}
			if err := writeFileAtomic(metadataPath, append(raw, '\n'), v.fileMode, v.dirMode); err != nil {
				return err
			}
		}
	}
	return writeFileAtomic(abs, data, v.fileMode, v.dirMode)
}

// Move the sidecar in the same transaction as its note or folder. If metadata
// cannot move, keep the content at its original path rather than losing its date.
func (v *Vault) relocateWithMetadata(from, to string, directory bool) error {
	if from == to {
		return nil
	}
	fromMetadata, err := v.metadataPath(v.relPosix(from), directory)
	if err != nil {
		return err
	}
	toMetadata, err := v.metadataPath(v.relPosix(to), directory)
	if err != nil {
		return err
	}
	hasMetadata := false
	if _, err := os.Lstat(fromMetadata); err == nil {
		hasMetadata = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err := os.Lstat(toMetadata); err == nil {
		return fmt.Errorf("destination metadata already exists: %s", toMetadata)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if hasMetadata {
		if err := os.MkdirAll(filepath.Dir(toMetadata), v.dirMode); err != nil {
			return err
		}
		if err := os.Rename(fromMetadata, toMetadata); err != nil {
			return err
		}
	}
	if err := os.Rename(from, to); err != nil {
		if hasMetadata {
			if rollback := os.Rename(toMetadata, fromMetadata); rollback != nil {
				return fmt.Errorf("note move failed (%v), metadata rollback failed: %w", err, rollback)
			}
		}
		return err
	}
	return nil
}

func (v *Vault) removeMetadata(rel string, directory bool) error {
	abs, err := v.metadataPath(rel, directory)
	if err != nil {
		return err
	}
	if directory {
		return os.RemoveAll(abs)
	}
	if err := os.Remove(abs); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
