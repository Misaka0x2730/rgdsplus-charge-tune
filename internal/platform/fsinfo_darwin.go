package platform

import (
	"path/filepath"

	"golang.org/x/sys/unix"
)

func statfs(path string) (FSInfo, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return FSInfo{}, err
	}
	name := make([]byte, 0, len(st.Fstypename))
	for _, c := range st.Fstypename {
		if c == 0 {
			break
		}
		name = append(name, byte(c))
	}
	return FSInfo{
		Free: st.Bavail * uint64(st.Bsize),
		Type: string(name),
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
