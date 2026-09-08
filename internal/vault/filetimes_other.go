//go:build !darwin && !windows

package vault

import "os"

// fileTimes returns the creation and modification times in epoch
// milliseconds. Plain stat carries no birth time on these platforms, so the
// change time stands in for it, which is what the desktop falls back to.
func fileTimes(info os.FileInfo) (createdMs, updatedMs int64) {
	updatedMs = info.ModTime().UnixMilli()
	return ctimeMs(info, updatedMs), updatedMs
}
