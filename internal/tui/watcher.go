package tui

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// vaultWatcher batches file-system events under the vault root.
type vaultWatcher struct {
	root    string
	w       *fsnotify.Watcher
	batches chan []string
	closeCh chan struct{}
	once    sync.Once
}

func startWatcher(root string) (*vaultWatcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	vw := &vaultWatcher{root: root, w: w, batches: make(chan []string, 4), closeCh: make(chan struct{})}
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDir(d.Name()) && path != root {
				return filepath.SkipDir
			}
			_ = w.Add(path)
		}
		return nil
	})
	go vw.loop()
	return vw, nil
}

func skipDir(name string) bool {
	return name == ".git" || name == "node_modules" || name == ".obsidian" || name == ".trash"
}

func (vw *vaultWatcher) loop() {
	pending := map[string]bool{}
	var timer *time.Timer
	var timerCh <-chan time.Time
	flush := func() {
		if len(pending) == 0 {
			return
		}
		paths := make([]string, 0, len(pending))
		for p := range pending {
			paths = append(paths, p)
		}
		pending = map[string]bool{}
		select {
		case vw.batches <- paths:
		case <-vw.closeCh:
		}
	}
	for {
		select {
		case <-vw.closeCh:
			return
		case ev, ok := <-vw.w.Events:
			if !ok {
				return
			}
			name := ev.Name
			base := filepath.Base(name)
			if strings.HasSuffix(base, ".tmp") || strings.HasPrefix(base, ".DS_Store") {
				continue
			}
			if ev.Has(fsnotify.Create) {
				if info, err := os.Stat(name); err == nil && info.IsDir() && !skipDir(base) {
					_ = vw.w.Add(name)
				}
			}
			rel, err := filepath.Rel(vw.root, name)
			if err != nil {
				continue
			}
			rel = filepath.ToSlash(rel)
			pending[rel] = true
			if timer == nil {
				timer = time.NewTimer(300 * time.Millisecond)
			} else {
				timer.Reset(300 * time.Millisecond)
			}
			timerCh = timer.C
		case <-timerCh:
			timerCh = nil
			flush()
		case _, ok := <-vw.w.Errors:
			if !ok {
				return
			}
		}
	}
}

// next blocks until a batch of changed vault-relative paths arrives.
func (vw *vaultWatcher) next() ([]string, bool) {
	select {
	case paths, ok := <-vw.batches:
		return paths, ok
	case <-vw.closeCh:
		return nil, false
	}
}

func (vw *vaultWatcher) close() {
	vw.once.Do(func() {
		close(vw.closeCh)
		_ = vw.w.Close()
	})
}
