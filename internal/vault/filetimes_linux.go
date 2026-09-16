package vault

import (
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// Linux's ordinary stat omits birth time. statx reports it on filesystems
// that support it; ctime is only a fallback, since chmod changes ctime.
func fileTimes(abs string, info os.FileInfo) (createdMs, updatedMs int64) {
	updatedMs = info.ModTime().UnixMilli()
	var extended unix.Statx_t
	if err := unix.Statx(unix.AT_FDCWD, abs, 0, unix.STATX_BTIME|unix.STATX_INO, &extended); err == nil && extended.Mask&unix.STATX_BTIME != 0 {
		previous, ok := info.Sys().(*syscall.Stat_t)
		if !ok || previous.Ino == extended.Ino {
			createdMs = extended.Btime.Sec*1000 + int64(extended.Btime.Nsec)/1_000_000
			if createdMs > 0 {
				return createdMs, updatedMs
			}
		}
	}
	return ctimeMs(info, updatedMs), updatedMs
}

func ctimeMs(info os.FileInfo, fallback int64) int64 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		ms := st.Ctim.Sec*1000 + st.Ctim.Nsec/1_000_000
		if ms > 0 {
			return ms
		}
	}
	return fallback
}
