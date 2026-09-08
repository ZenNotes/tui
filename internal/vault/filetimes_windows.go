package vault

import (
	"os"
	"syscall"
)

// fileTimes returns the creation and modification times in epoch
// milliseconds. Windows records a creation time in the file attributes.
func fileTimes(info os.FileInfo) (createdMs, updatedMs int64) {
	updatedMs = info.ModTime().UnixMilli()
	if st, ok := info.Sys().(*syscall.Win32FileAttributeData); ok {
		createdMs = st.CreationTime.Nanoseconds() / 1_000_000
		if createdMs > 0 {
			return createdMs, updatedMs
		}
	}
	return updatedMs, updatedMs
}
