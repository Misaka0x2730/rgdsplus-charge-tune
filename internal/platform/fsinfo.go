package platform

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// FAT32MaxFileSize is the largest file FAT32 can store (4 GiB - 1).
const FAT32MaxFileSize = 1<<32 - 1

// FSInfo describes the filesystem holding a path.
type FSInfo struct {
	Free uint64 // bytes available to unprivileged users
	Type string // "vfat", "exfat", "ext4", "apfs", ... ("" if unknown)
}

// SameFilesystem reports whether two existing paths are on one filesystem,
// so that a rename can move files between them.
func SameFilesystem(a, b string) (bool, error) {
	sa, err := os.Stat(a)
	if err != nil {
		return false, err
	}
	sb, err := os.Stat(b)
	if err != nil {
		return false, err
	}
	da, okA := sa.Sys().(*syscall.Stat_t)
	db, okB := sb.Sys().(*syscall.Stat_t)
	if !okA || !okB {
		return true, nil // no device numbers here: let the rename decide
	}
	return da.Dev == db.Dev, nil
}

// IsFAT32 reports whether files are limited to 4 GiB.
func (i FSInfo) IsFAT32() bool {
	return i.Type == "vfat" || i.Type == "msdos"
}

// Stat returns filesystem information for the nearest existing ancestor of
// path (the target folder may not exist yet).
func Stat(path string) (FSInfo, error) {
	p := path
	for {
		if _, err := os.Stat(p); err == nil {
			break
		}
		parent := filepath.Dir(p)
		if parent == p {
			break
		}
		p = parent
	}
	return statfs(p)
}

// NoSpaceError means the target filesystem is too full.
type NoSpaceError struct{ Need, Free int64 }

func (e *NoSpaceError) Error() string {
	return fmt.Sprintf("not enough free space: need %d bytes, %d available", e.Need, e.Free)
}

// FAT32LimitError means a file is too big for a FAT32 card.
type FAT32LimitError struct{ Size int64 }

func (e *FAT32LimitError) Error() string {
	return fmt.Sprintf("a file of %d bytes exceeds the FAT32 limit of 4 GiB", e.Size)
}

// CheckSpace verifies that need bytes fit on the filesystem holding dir and
// that no single file (largest) breaks the FAT32 limit. If the filesystem
// cannot be queried the check passes; the write itself will fail if it must.
func CheckSpace(dir string, need, largest int64) error {
	info, err := Stat(dir)
	if err != nil {
		return nil
	}
	if largest > FAT32MaxFileSize && info.IsFAT32() {
		return &FAT32LimitError{Size: largest}
	}
	if need > 0 && int64(info.Free) < need {
		return &NoSpaceError{Need: need, Free: int64(info.Free)}
	}
	return nil
}
