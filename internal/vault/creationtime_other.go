//go:build !darwin

package vault

import "io/fs"

func preserveCreationTime(_ string, _ fs.FileInfo) error { return nil }
