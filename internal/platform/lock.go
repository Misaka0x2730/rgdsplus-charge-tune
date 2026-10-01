package platform

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// ErrAlreadyRunning means another copy of the app holds the lock.
var ErrAlreadyRunning = errors.New("the app is already running")

// Lock takes an exclusive lock on path for the life of the process, so two
// copies never write the boot partition at once. The lock goes away with
// the process, even if it is killed.
func Lock(path string) (release func(), err error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, ErrAlreadyRunning
		}
		return nil, err
	}
	return func() {
		_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
		f.Close()
	}, nil
}
