package platform

import (
	"path/filepath"

	"golang.org/x/sys/unix"
)

var fsMagic = map[int64]string{
	0x4d44:     "vfat",
	0x2011bab0: "exfat",
	0xef53:     "ext4", // ext2/3/4 share the magic
	0x5346544e: "ntfs",
	0x7366746e: "ntfs3",
	0xf2f52010: "f2fs",
	0x01021994: "tmpfs",
	0x9123683e: "btrfs",
	0x58465342: "xfs",
}

func statfs(path string) (FSInfo, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return FSInfo{}, err
	}
	return FSInfo{
		Free: uint64(st.Bavail) * uint64(st.Bsize),
		Type: fsMagic[int64(st.Type)],
	}, nil
}

func isMountPoint(path string) bool {
	var self, parent unix.Stat_t
	if err := unix.Stat(path, &self); err != nil {
		return false
	}
	if err := unix.Stat(filepath.Dir(path), &parent); err != nil {
		return false
	}
	return self.Dev != parent.Dev || self.Ino == parent.Ino
}
