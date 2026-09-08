package vault

import (
	"os"
	"syscall"
)

func ctimeMs(info os.FileInfo, fallback int64) int64 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		ms := st.Ctim.Sec*1000 + st.Ctim.Nsec/1_000_000
		if ms > 0 {
			return ms
		}
	}
	return fallback
}
