package flash

import (
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

// blockSize asks the kernel for the size of a block device in bytes.
func blockSize(path string) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	var size uint64
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, f.Fd(), unix.BLKGETSIZE64, uintptr(unsafe.Pointer(&size))); errno != 0 {
		return 0, errno
	}
	return int64(size), nil
}

// dropBlockCache writes out and forgets the kernel's cached blocks of the
// device, so the next read comes from the card.
func dropBlockCache(f *os.File) {
	_, _, _ = unix.Syscall(unix.SYS_IOCTL, f.Fd(), unix.BLKFLSBUF, 0)
}

// dropFileCache forgets the cached pages of a file that was just synced,
// so reading it again comes from the card.
func dropFileCache(f *os.File) {
	_ = unix.Fadvise(int(f.Fd()), 0, 0, unix.FADV_DONTNEED)
}
