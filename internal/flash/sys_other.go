//go:build !linux

package flash

import (
	"errors"
	"os"
)

// Development computers have no boot partition; images are plain files.

func blockSize(string) (int64, error) {
	return 0, errors.New("block devices are only supported on Linux")
}

func dropBlockCache(*os.File) {}

func dropFileCache(*os.File) {}
