// Package logging sets up the application logger: a size-rotated file on the
// device, stdout in dev mode.
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

// DefaultMaxSize is the size at which the log file is rotated (plan: 1 MB).
const DefaultMaxSize = 1 << 20

// RotatingFile is an io.Writer that keeps one backup (<name>.1) and rotates
// when the current file would exceed MaxSize.
type RotatingFile struct {
	path    string
	maxSize int64

	mu   sync.Mutex
	f    *os.File
	size int64
}

// OpenRotating opens (or creates) path for appending.
func OpenRotating(path string, maxSize int64) (*RotatingFile, error) {
	if maxSize <= 0 {
		maxSize = DefaultMaxSize
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	r := &RotatingFile{path: path, maxSize: maxSize}
	if err := r.open(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *RotatingFile) open() error {
	f, err := os.OpenFile(r.path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	r.f, r.size = f, st.Size()
	return nil
}

func (r *RotatingFile) rotate() error {
	if r.f != nil {
		r.f.Close()
		r.f = nil
	}
	backup := r.path + ".1"
	_ = os.Remove(backup)
	if err := os.Rename(r.path, backup); err != nil && !os.IsNotExist(err) {
		return err
	}
	return r.open()
}

func (r *RotatingFile) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		return 0, os.ErrClosed
	}
	if r.size > 0 && r.size+int64(len(p)) > r.maxSize {
		if err := r.rotate(); err != nil {
			return 0, fmt.Errorf("rotate log: %w", err)
		}
	}
	n, err := r.f.Write(p)
	r.size += int64(n)
	return n, err
}

// Close closes the underlying file.
func (r *RotatingFile) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		return nil
	}
	err := r.f.Close()
	r.f = nil
	return err
}

// Setup installs the default slog logger. In dev mode it logs to stdout at
// debug level; otherwise to a rotated file at info level. The returned closer
// flushes/closes the file.
func Setup(dev bool, path string) (io.Closer, error) {
	if dev {
		h := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})
		slog.SetDefault(slog.New(h))
		return io.NopCloser(nil), nil
	}
	rf, err := OpenRotating(path, DefaultMaxSize)
	if err != nil {
		// Still log somewhere so a broken SD card doesn't hide everything.
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))
		return io.NopCloser(nil), err
	}
	level := slog.LevelInfo
	if os.Getenv("CHARGETUNE_DEBUG") != "" {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(rf, &slog.HandlerOptions{Level: level})))
	return rf, nil
}
