// Command rgdsplus-charge-tune sets the charge current and the input
// current limit of the Anbernic RG DS Plus by patching the charger values in
// the kernel device tree of the boot partition.
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"

	"rgdsplus-charge-tune/internal/app"
	"rgdsplus-charge-tune/internal/logging"
	"rgdsplus-charge-tune/internal/platform"
	"rgdsplus-charge-tune/internal/version"
)

func main() {
	var (
		dev         = flag.Bool("dev", false, "development mode: 1024x768 window, logs to stdout")
		root        = flag.String("root", "", "filesystem prefix for device paths, e.g. ./devroot")
		screen      = flag.Int("screen", -1, "SDL display index to show the UI on (-1 = the system's main screen)")
		rotate      = flag.Int("rotate", 0, "clockwise display rotation: 0, 90, 180 or 270")
		dataDir     = flag.String("data", "", "data directory (default: <binary dir>/data, ./devdata with -dev)")
		showVersion = flag.Bool("version", false, "print version and exit")
		status      = flag.Bool("status", false, "print the charger values, the boot partition and the backups as JSON, then exit (reads only)")
		patchIn     = flag.String("patch-in", "", "offline: boot image file to patch (never a device)")
		patchOut    = flag.String("patch-out", "", "offline: where to write the patched image")
		mode        = flag.String("mode", "", "offline: mode id for -patch-in (see -status)")
		charge      = flag.Int("charge", 0, "offline: charge current in mA instead of -mode")
		input       = flag.Int("input", 0, "offline: input current limit in mA instead of -mode")
	)
	flag.Parse()

	if *showVersion {
		fmt.Println("rgdsplus-charge-tune", version.String())
		return
	}
	// 1 GB of RAM is shared with the frontend; the app holds one or two
	// 64 MB images at a time.
	debug.SetMemoryLimit(300 << 20)

	data := resolveDataDir(*dataDir, *dev)
	if *patchIn != "" || *status {
		// Command-line modes log to stderr and leave the data folder alone.
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})))
		plat, err := platform.Load(platform.Options{Root: *root, DataDir: data, Dev: *dev})
		if err != nil {
			fail(err)
		}
		if *patchIn != "" {
			if err := runPatch(plat, *patchIn, *patchOut, *mode, *charge, *input); err != nil {
				fail(err)
			}
			return
		}
		if err := runStatus(plat, data); err != nil {
			fail(err)
		}
		return
	}

	if *dev {
		// gabagool reads this to open a 1024x768 desktop window.
		os.Setenv("ENVIRONMENT", "DEV")
	}
	if err := os.MkdirAll(data, 0o755); err != nil {
		fail(fmt.Errorf("cannot create data dir: %w", err))
	}
	logCloser, err := logging.Setup(*dev, filepath.Join(data, "rgdsplus-charge-tune.log"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "log setup:", err)
	}
	defer logCloser.Close()
	slog.Info("starting", "version", version.String(), "dev", *dev, "root", *root, "data", data)

	lockPath := filepath.Join(os.TempDir(), "rgdsplus-charge-tune.lock")
	if *dev || *root != "" {
		lockPath = filepath.Join(data, ".lock")
	}
	unlock, err := platform.Lock(lockPath)
	if err != nil {
		slog.Error("lock", "path", lockPath, "err", err)
		logCloser.Close()
		fail(err)
	}
	defer unlock()

	plat, err := platform.Load(platform.Options{Root: *root, DataDir: data, Dev: *dev})
	if err != nil {
		fail(err)
	}
	err = app.Run(plat, app.Options{Dev: *dev, Root: *root, DataDir: data, Screen: *screen, Rotate: *rotate})
	if err != nil {
		slog.Error("exited with error", "err", err)
		logCloser.Close()
		os.Exit(1)
	}
	slog.Info("exited")
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "rgdsplus-charge-tune:", err)
	os.Exit(1)
}

func resolveDataDir(flagValue string, dev bool) string {
	if flagValue != "" {
		abs, err := filepath.Abs(flagValue)
		if err == nil {
			return abs
		}
		return flagValue
	}
	if dev {
		abs, err := filepath.Abs("devdata")
		if err == nil {
			return abs
		}
		return "devdata"
	}
	exe, err := os.Executable()
	if err != nil {
		return "data"
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Join(filepath.Dir(exe), "data")
}
