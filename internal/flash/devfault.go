package flash

import "os"

// InjectFaults makes writes of the boot partition read back wrong, to show
// the error screens on a development computer (the app calls it only with
// -dev, from CHARGETUNE_FAKE_WRITE_FAULT):
//
//	verify  the new data never reads back; putting the old content back works
//	all     putting the old content back fails as well
func InjectFaults(mode string) {
	if mode != "verify" && mode != "all" {
		return
	}
	afterWrite = func(path string, attempt int) {
		if attempt == attemptRestore && mode != "all" {
			return
		}
		f, err := os.OpenFile(path, os.O_RDWR, 0)
		if err != nil {
			return
		}
		defer f.Close()
		b := make([]byte, 1)
		f.ReadAt(b, 0)
		b[0] ^= 0xff
		f.WriteAt(b, 0)
	}
}
