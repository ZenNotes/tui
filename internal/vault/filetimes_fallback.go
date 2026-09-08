//go:build !darwin && !windows && !linux

package vault

import "os"

func ctimeMs(info os.FileInfo, fallback int64) int64 {
	return fallback
}
