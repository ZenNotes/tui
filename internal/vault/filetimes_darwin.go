package vault

import (
	"os"
	"syscall"
)

// fileTimes returns the creation and modification times in epoch
// milliseconds. macOS keeps a real birth time, which is what the desktop
// reports as createdAt.
func fileTimes(info os.FileInfo) (createdMs, updatedMs int64) {
	updatedMs = info.ModTime().UnixMilli()
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		createdMs = st.Birthtimespec.Sec*1000 + st.Birthtimespec.Nsec/1_000_000
		if createdMs > 0 {
			return createdMs, updatedMs
		}
	}
	return updatedMs, updatedMs
}
