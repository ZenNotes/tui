package vault

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"syscall"
	"time"
)

// The scratch file writeFileAtomic renames from: `<target>.<pid>.<nanos>.tmp`.
// The shape is shared with the desktop app and the server, and every watcher
// filters on it, so the three must stay recognizable to each other.
var atomicWriteTempPattern = regexp.MustCompile(`\.\d+\.\d{13,}\.tmp$`)

// IsAtomicWriteTempPath reports whether p is one of those scratch files.
func IsAtomicWriteTempPath(p string) bool {
	return atomicWriteTempPattern.MatchString(filepath.Base(p))
}

// writeFileAtomic writes data by way of a temp file in the same directory,
// fsynced, then renamed over the target, so no reader (the app's watcher
// echoing a save back) can ever observe a truncated note. A symlinked note is
// written THROUGH, and an existing file keeps its own permissions.
func writeFileAtomic(abs string, data []byte, fileMode, dirMode fs.FileMode) error {
	target, err := resolveLinkTarget(abs)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), dirMode); err != nil {
		return err
	}
	mode := fileMode
	replacing := false
	if info, statErr := os.Stat(target); statErr == nil {
		mode = info.Mode().Perm()
		replacing = true
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	temp := fmt.Sprintf("%s.%d.%d.tmp", target, os.Getpid(), time.Now().UnixNano())
	f, err := os.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(temp)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(temp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(temp)
		return err
	}
	if replacing {
		if err := os.Chmod(temp, mode); err != nil {
			_ = os.Remove(temp)
			return err
		}
	}
	if err := renameWithRetry(temp, target); err != nil {
		_ = os.Remove(temp)
		return err
	}
	return nil
}

const atomicRenameAttempts = 20
const windowsSharingViolation syscall.Errno = 32

func transientRenameError(err error) bool {
	if errors.Is(err, fs.ErrPermission) {
		return true
	}
	var errno syscall.Errno
	return runtime.GOOS == "windows" && errors.As(err, &errno) && errno == windowsSharingViolation
}

// Windows refuses a replace while any reader has the destination open; wait
// for the handle instead of failing the save.
func renameWithRetry(from, to string) error {
	delay := time.Millisecond
	for attempt := 1; ; attempt++ {
		err := os.Rename(from, to)
		if err == nil {
			return nil
		}
		if attempt >= atomicRenameAttempts || !transientRenameError(err) {
			return err
		}
		time.Sleep(delay)
		delay = min(delay*2, 25*time.Millisecond)
	}
}

func resolveLinkTarget(abs string) (string, error) {
	info, err := os.Lstat(abs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return abs, nil
		}
		return "", err
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return abs, nil
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err == nil {
		return resolved, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	dest, err := os.Readlink(abs)
	if err != nil {
		return "", err
	}
	if filepath.IsAbs(dest) {
		return dest, nil
	}
	return filepath.Join(filepath.Dir(abs), dest), nil
}
