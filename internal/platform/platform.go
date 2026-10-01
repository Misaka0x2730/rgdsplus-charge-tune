// Package platform describes the device: where the boot partition, the
// live device tree and the battery are, where backups may go, which modes
// exist, and the system settings the app follows (language, screen swap,
// lid and sleep timer). Defaults for the RG DS Plus are embedded and can be
// overridden with <data>/platform.json without rebuilding.
package platform

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"rgdsplus-charge-tune/internal/charger"
)

//go:embed rgdsplus.json
var defaultConfigJSON []byte

// Location is a preset folder for backups on a memory card.
type Location struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Card  string `json:"card"` // the card's mount point
	Dir   string `json:"dir"`
}

// Config is the JSON-serialisable device description. Paths are device
// paths; the -root development prefix is applied when they are used.
type Config struct {
	Name string `json:"name"`

	// The model: the first line of BoardFile is one of BoardNames, and the
	// live device tree's root is compatible with SoCCompatible.
	BoardFile     string   `json:"board_file,omitempty"`
	BoardNames    []string `json:"board_names,omitempty"`
	FirmwareFile  string   `json:"firmware_file,omitempty"`
	SoCCompatible string   `json:"soc_compatible,omitempty"`

	// The live device tree and the charger node in it.
	DeviceTree  string `json:"device_tree"`
	ChargerNode string `json:"charger_node"`

	// The boot partition and its expected size (0: any).
	BootPartition string `json:"boot_partition"`
	BootSize      int64  `json:"boot_size,omitempty"`

	BackupLocations []Location `json:"backup_locations"`

	// Writing is refused below MinBatteryPercent unless a charger is
	// connected (any of ChargerOnlineFiles reads 1).
	BatteryCapacityFile string   `json:"battery_capacity_file,omitempty"`
	ChargerOnlineFiles  []string `json:"charger_online_files,omitempty"`
	MinBatteryPercent   int      `json:"min_battery_percent,omitempty"`

	// Modes offered; StockMode names the factory values.
	StockMode string         `json:"stock_mode"`
	Modes     []charger.Mode `json:"modes"`

	// Shell commands run around writing. Empty = do nothing.
	InhibitSleepCmd string `json:"inhibit_sleep_cmd,omitempty"`
	AllowSleepCmd   string `json:"allow_sleep_cmd,omitempty"`

	// The system language setting: a file whose first line is an index
	// into LanguageCodes (the stock firmware's /mnt/vendor/oem/language.ini).
	LanguageFile  string   `json:"language_file,omitempty"`
	LanguageCodes []string `json:"language_codes,omitempty"`

	// The system "swap screens" setting: a file that reads 1 when the main
	// screen is display 1 instead of 0 (the stock firmware's lcdswap).
	ScreenSwapFile string `json:"screen_swap_file,omitempty"`

	// The lid: LidFile reads LidClosedValue while the lid is shut (the stock
	// firmware's hall sensor), and SleepCmd suspends the console the way the
	// system does on a lid close. Opening the lid wakes it up.
	LidFile        string `json:"lid_file,omitempty"`
	LidClosedValue string `json:"lid_closed_value,omitempty"`
	SleepCmd       string `json:"sleep_cmd,omitempty"`

	// The system sleep timer: a little-endian int32 at SleepTimerOffset of
	// SleepTimerFile (only if the file is SleepTimerFileSize bytes, when set)
	// that indexes SleepTimerSeconds; 0 seconds means never.
	SleepTimerFile     string `json:"sleep_timer_file,omitempty"`
	SleepTimerFileSize int    `json:"sleep_timer_file_size,omitempty"`
	SleepTimerOffset   int    `json:"sleep_timer_offset,omitempty"`
	SleepTimerSeconds  []int  `json:"sleep_timer_seconds,omitempty"`
}

// Platform is the resolved runtime description.
type Platform struct {
	Config
	// Root is prepended to every device path (the -root dev flag).
	Root    string
	DataDir string
	Dev     bool
	// SetAside names the damaged files Load renamed to .bad (platform.json).
	SetAside []string
	// ModeProblems lists modes of the configuration that were dropped.
	ModeProblems []error
	// OnRootFS reports whether a path is on the system's root filesystem
	// (CheckBackupDir). nil compares device numbers with "/" on the console
	// and skips the check with -root or -dev, where the fake cards are plain
	// folders; tests set it to exercise the check.
	OnRootFS func(path string) (bool, error)

	sleepMu    sync.Mutex
	sleepHolds int
	lidSleep   atomic.Bool
	idleSleep  atomic.Bool
	lastActive atomic.Int64 // unix nanoseconds of the last input or work
}

// Options for Load.
type Options struct {
	Root    string // filesystem prefix for device paths ("" on the device)
	DataDir string // app data directory (settings, logs, overrides)
	Dev     bool
}

// Load builds the platform from embedded defaults plus an optional
// <DataDir>/platform.json override. A damaged override is renamed to .bad
// and the defaults are used (see Platform.SetAside).
func Load(opts Options) (*Platform, error) {
	var cfg Config
	if err := json.Unmarshal(defaultConfigJSON, &cfg); err != nil {
		return nil, fmt.Errorf("embedded platform config: %w", err)
	}
	var setAside []string
	if opts.DataDir != "" {
		override := filepath.Join(opts.DataDir, "platform.json")
		if data, err := os.ReadFile(override); err == nil {
			// Unmarshal over the defaults so the override may be partial.
			if err := json.Unmarshal(data, &cfg); err != nil {
				cause := fmt.Errorf("%s: %w", override, err)
				if os.Rename(override, override+".bad") != nil {
					return nil, cause
				}
				slog.Warn("damaged file renamed", "to", override+".bad", "err", cause)
				setAside = append(setAside, "platform.json")
				// A type error leaves cfg (and its slices) half overwritten.
				cfg = Config{}
				_ = json.Unmarshal(defaultConfigJSON, &cfg)
			} else {
				slog.Info("platform override loaded", "path", override)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	root := opts.Root
	if root != "" {
		abs, err := filepath.Abs(root)
		if err != nil {
			return nil, err
		}
		root = abs
	}
	p := &Platform{Config: cfg, Root: root, DataDir: opts.DataDir, Dev: opts.Dev, SetAside: setAside}
	p.Modes, p.ModeProblems = charger.ValidModes(cfg.Modes)
	for _, err := range p.ModeProblems {
		slog.Warn("mode dropped", "err", err)
	}
	return p, nil
}

// FSPath maps a device path from the config to the filesystem ("" stays "",
// which no read finds).
func (p *Platform) FSPath(devicePath string) string {
	if devicePath == "" || p.Root == "" {
		return filepath.FromSlash(devicePath)
	}
	return filepath.Join(p.Root, filepath.FromSlash(devicePath))
}

// DevicePath is the inverse of FSPath: what the console calls a host path.
func (p *Platform) DevicePath(hostPath string) string {
	if p.Root == "" {
		return hostPath
	}
	rel, err := filepath.Rel(p.Root, hostPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return hostPath
	}
	return "/" + filepath.ToSlash(rel)
}

func (p *Platform) firstLine(devicePath string) string {
	data, err := os.ReadFile(p.FSPath(devicePath))
	if err != nil {
		return ""
	}
	line, _, _ := strings.Cut(string(data), "\n")
	return strings.TrimSpace(line)
}

// Board returns the model name ("RGdsplus"), "" if unknown.
func (p *Platform) Board() string { return p.firstLine(p.BoardFile) }

// Firmware returns the firmware version ("20260915"), "" if unknown.
func (p *Platform) Firmware() string { return p.firstLine(p.FirmwareFile) }

// UnsupportedDeviceError means the app is not running on the console it was
// made for, so the boot partition must not be touched.
type UnsupportedDeviceError struct{ Board, Reason string }

func (e *UnsupportedDeviceError) Error() string {
	return fmt.Sprintf("unsupported device %q: %s", e.Board, e.Reason)
}

// CheckDevice verifies the model and the SoC of the running device tree.
func (p *Platform) CheckDevice() error {
	board := p.Board()
	if len(p.BoardNames) > 0 {
		ok := false
		for _, n := range p.BoardNames {
			if strings.EqualFold(board, n) {
				ok = true
			}
		}
		if !ok {
			return &UnsupportedDeviceError{Board: board, Reason: "not " + strings.Join(p.BoardNames, "/")}
		}
	}
	if p.SoCCompatible != "" {
		data, err := os.ReadFile(filepath.Join(p.FSPath(p.DeviceTree), "compatible"))
		if err != nil {
			return &UnsupportedDeviceError{Board: board, Reason: "no device tree: " + err.Error()}
		}
		found := false
		for _, s := range strings.Split(strings.TrimRight(string(data), "\x00"), "\x00") {
			if s == p.SoCCompatible {
				found = true
			}
		}
		if !found {
			return &UnsupportedDeviceError{Board: board, Reason: "not " + p.SoCCompatible}
		}
	}
	return nil
}

// RunningValues are the charger settings the kernel was booted with.
func (p *Platform) RunningValues() (charger.Values, error) {
	v, _, err := charger.ReadRunning(p.FSPath(p.DeviceTree), p.ChargerNode)
	return v, err
}

// BootPath is the boot partition on the filesystem.
func (p *Platform) BootPath() string { return p.FSPath(p.BootPartition) }

// Stock returns the factory mode.
func (p *Platform) Stock() (charger.Mode, bool) {
	for _, m := range p.Modes {
		if m.ID == p.StockMode {
			return m, true
		}
	}
	return charger.Mode{}, false
}

// Location plus whether its card is there, with host paths.
type AvailableLocation struct {
	Location
	Present bool
}

// Locations lists the preset backup folders; Dir and Card are host paths.
func (p *Platform) Locations() []AvailableLocation {
	var out []AvailableLocation
	for _, l := range p.BackupLocations {
		l.Card, l.Dir = p.FSPath(l.Card), p.FSPath(l.Dir)
		out = append(out, AvailableLocation{Location: l, Present: p.cardPresent(l.Card)})
	}
	return out
}

// cardPresent: on the device a card is there when its mount point is a
// mount point; with -root (development) when the folder exists.
func (p *Platform) cardPresent(card string) bool {
	st, err := os.Stat(card)
	if err != nil || !st.IsDir() {
		return false
	}
	if p.Root != "" || p.Dev {
		return true
	}
	return isMountPoint(card)
}

// DefaultBackupDir is the first preset folder whose card is present, else
// the first preset (host path).
func (p *Platform) DefaultBackupDir() string {
	locs := p.Locations()
	for _, l := range locs {
		if l.Present {
			return l.Dir
		}
	}
	if len(locs) > 0 {
		return locs[0].Dir
	}
	return filepath.Join(p.DataDir, "backups")
}

// BackupSearchDirs are the folders where backups are looked for: the chosen
// one first, then every preset.
func (p *Platform) BackupSearchDirs(chosen string) []string {
	out := []string{chosen}
	for _, l := range p.Locations() {
		if l.Dir != chosen {
			out = append(out, l.Dir)
		}
	}
	return out
}

// ErrNotOnCard means a backup folder is on the system's root filesystem:
// with the card missing, /mnt/sdcard is an empty folder of the rootfs, and
// a backup there would not be where the user looks for it.
var ErrNotOnCard = errors.New("the backup folder is not on a memory card")

// CheckBackupDir refuses folders on the root filesystem. The folder may not
// exist yet: its nearest existing parent is checked.
func (p *Platform) CheckBackupDir(dir string) error {
	onRoot := p.OnRootFS
	if onRoot == nil {
		if p.Root != "" || p.Dev {
			return nil
		}
		onRoot = func(path string) (bool, error) { return SameFilesystem(path, "/") }
	}
	existing := dir
	for {
		if _, err := os.Stat(existing); err == nil {
			break
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			break
		}
		existing = parent
	}
	same, err := onRoot(existing)
	if err != nil {
		return err
	}
	if same {
		return ErrNotOnCard
	}
	return nil
}

// Power is the battery state.
type Power struct {
	Percent int  `json:"percent"` // -1 if unknown
	Charger bool `json:"charger"` // a charger or USB port is connected
}

// PowerState reads the battery level and the charger.
func (p *Platform) PowerState() Power {
	pw := Power{Percent: -1}
	if v, err := strconv.Atoi(p.firstLine(p.BatteryCapacityFile)); err == nil && p.BatteryCapacityFile != "" {
		pw.Percent = v
	}
	for _, f := range p.ChargerOnlineFiles {
		if p.firstLine(f) == "1" {
			pw.Charger = true
		}
	}
	return pw
}

// LowBatteryError means the battery is too low to write without a charger.
type LowBatteryError struct{ Percent, Min int }

func (e *LowBatteryError) Error() string {
	return fmt.Sprintf("battery at %d%%, below %d%% without a charger", e.Percent, e.Min)
}

// CheckPower refuses writing on a low battery without a charger. An
// unknown level counts as fine only in development.
func (p *Platform) CheckPower() error {
	pw := p.PowerState()
	if pw.Charger || p.MinBatteryPercent <= 0 {
		return nil
	}
	if pw.Percent < 0 {
		if p.Dev {
			return nil
		}
		return &LowBatteryError{Percent: pw.Percent, Min: p.MinBatteryPercent}
	}
	if pw.Percent < p.MinBatteryPercent {
		return &LowBatteryError{Percent: pw.Percent, Min: p.MinBatteryPercent}
	}
	return nil
}

// RequestReboot leaves a flag for the launcher, which reboots the console
// after the app exits (so that the UI and its files are closed first).
func (p *Platform) RequestReboot() error {
	if p.DataDir == "" {
		return errors.New("no data folder")
	}
	return os.WriteFile(filepath.Join(p.DataDir, ".reboot"), nil, 0o644)
}

// ScreensSwapped reports whether the system settings put the main screen on
// display 1.
func (p *Platform) ScreensSwapped() bool {
	return p.firstLine(p.ScreenSwapFile) == "1"
}

// SystemLanguage returns the language chosen in the device's own settings
// ("ru", "en", ...), or "" when the platform does not tell.
func (p *Platform) SystemLanguage() string {
	i, err := strconv.Atoi(p.firstLine(p.LanguageFile))
	if err != nil || i < 0 || i >= len(p.LanguageCodes) {
		return ""
	}
	return p.LanguageCodes[i]
}

// InhibitSleep keeps the console awake until the returned release is
// called: take it around work (reading and writing the partition), not
// around waiting for the user. Calls nest: the inhibit command runs on the
// first call and the allow command after the last release; the sleep
// watcher does not suspend while any hold is active, and its idle timer
// starts over when the last hold is released.
func (p *Platform) InhibitSleep() (release func()) {
	p.sleepMu.Lock()
	p.sleepHolds++
	if p.sleepHolds == 1 && p.InhibitSleepCmd != "" {
		runShell(p.InhibitSleepCmd)
	}
	p.sleepMu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			p.sleepMu.Lock()
			defer p.sleepMu.Unlock()
			p.sleepHolds--
			if p.sleepHolds == 0 {
				p.Touch()
				if p.AllowSleepCmd != "" {
					runShell(p.AllowSleepCmd)
				}
			}
		})
	}
}

func runShell(cmd string) {
	out, err := exec.Command("/bin/sh", "-c", cmd).CombinedOutput()
	if err != nil {
		slog.Warn("platform command failed", "cmd", cmd, "err", err, "out", string(out))
	}
}
