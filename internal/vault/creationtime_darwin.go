package vault

import (
	"encoding/binary"
	"io/fs"
	"syscall"

	"golang.org/x/sys/unix"
)

// Atomic replacement creates a new inode. Carry the old birth time onto the
// staged file before rename so desktop sorting and CLI metadata keep its age.
func preserveCreationTime(staged string, previous fs.FileInfo) error {
	stat, ok := previous.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	var data [16]byte
	binary.NativeEndian.PutUint64(data[:8], uint64(stat.Birthtimespec.Sec))
	binary.NativeEndian.PutUint64(data[8:], uint64(stat.Birthtimespec.Nsec))
	attrs := unix.Attrlist{Bitmapcount: unix.ATTR_BIT_MAP_COUNT, Commonattr: unix.ATTR_CMN_CRTIME}
	return unix.Setattrlist(staged, &attrs, data[:], 0)
}
