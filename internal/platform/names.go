package platform

import "strings"

// SafeFileName makes a typed folder name valid on the FAT32/exFAT cards:
// characters those filesystems reject ( " * / : < > ? \ | and control
// characters) become "_", and trailing dots and spaces are dropped (FAT
// silently strips them, which would break the later rename).
func SafeFileName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r < 0x20, r == 0x7f:
			b.WriteRune('_')
		case strings.ContainsRune(`"*/:<>?\|`, r):
			b.WriteRune('_')
		default:
			b.WriteRune(r)
		}
	}
	out := strings.TrimRight(b.String(), ". ")
	if out == "" || out == "." || out == ".." {
		return "_"
	}
	return out
}
