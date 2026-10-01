package platform

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testPlatform is the embedded configuration over an empty fake device in a
// temporary folder (-root), in development mode.
func testPlatform(t *testing.T) *Platform {
	t.Helper()
	p, err := Load(Options{Root: t.TempDir(), DataDir: t.TempDir(), Dev: true})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func write(t *testing.T, p *Platform, devicePath string, data []byte) {
	t.Helper()
	path := p.FSPath(devicePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func cell(v uint32) []byte { return binary.BigEndian.AppendUint32(nil, v) }

func TestEmbeddedConfig(t *testing.T) {
	p := testPlatform(t)
	if len(p.ModeProblems) != 0 {
		t.Fatalf("embedded modes rejected: %v", p.ModeProblems)
	}
	want := []string{"stock", "play_charging", "fast", "gentle"}
	if len(p.Modes) != len(want) {
		t.Fatalf("modes %+v", p.Modes)
	}
	for i, id := range want {
		if p.Modes[i].ID != id {
			t.Errorf("mode %d is %s, want %s", i, p.Modes[i].ID, id)
		}
	}
	if s, ok := p.Stock(); !ok || s.ChargeCurrent != 2000 || s.InputCurrent != 1500 {
		t.Fatalf("stock %+v", s)
	}
	if p.BootSize != 64<<20 || p.BootPartition != "/dev/block/by-name/boot" {
		t.Fatalf("boot %s %d", p.BootPartition, p.BootSize)
	}
}

func TestOverrideDropsUnsafeModes(t *testing.T) {
	data := t.TempDir()
	os.WriteFile(filepath.Join(data, "platform.json"), []byte(`{"modes":[
		{"id":"stock","charge_ma":2000,"input_ma":1500},
		{"id":"hot","charge_ma":3500,"input_ma":3000}]}`), 0o644)
	p, err := Load(Options{Root: t.TempDir(), DataDir: data, Dev: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Modes) != 1 || len(p.ModeProblems) != 1 {
		t.Fatalf("modes %+v, problems %v", p.Modes, p.ModeProblems)
	}
	if p.BootPartition == "" {
		t.Fatal("a partial override lost the defaults")
	}

	bad := t.TempDir()
	os.WriteFile(filepath.Join(bad, "platform.json"), []byte(`{"modes": 5}`), 0o644)
	p, err = Load(Options{DataDir: bad})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.SetAside) != 1 || len(p.Modes) != 4 {
		t.Fatalf("damaged override: set aside %v, %d modes", p.SetAside, len(p.Modes))
	}
}

func TestCheckDevice(t *testing.T) {
	p := testPlatform(t)
	var unsupported *UnsupportedDeviceError
	if err := p.CheckDevice(); !errors.As(err, &unsupported) {
		t.Fatalf("empty device: %v", err)
	}
	write(t, p, p.BoardFile, []byte("RGdsplus\n"))
	if err := p.CheckDevice(); !errors.As(err, &unsupported) {
		t.Fatalf("no device tree: %v", err)
	}
	write(t, p, p.DeviceTree+"/compatible", []byte("rockchip,rk3568-deep-lp3-v10\x00rockchip,rk3568\x00"))
	if err := p.CheckDevice(); err != nil {
		t.Fatal(err)
	}
	write(t, p, p.BoardFile, []byte("RGds\n"))
	if err := p.CheckDevice(); !errors.As(err, &unsupported) {
		t.Fatalf("RG DS (not Plus): %v", err)
	}
}

func TestRunningValuesAndPower(t *testing.T) {
	p := testPlatform(t)
	node := p.DeviceTree + p.ChargerNode
	write(t, p, node+"/compatible", []byte("rk817,charger\x00"))
	write(t, p, node+"/max_chrg_current", cell(2000))
	write(t, p, node+"/max_input_current", cell(1500))
	write(t, p, node+"/max_chrg_voltage", cell(4400))
	v, err := p.RunningValues()
	if err != nil || v.ChargeCurrent != 2000 || v.InputCurrent != 1500 || v.ChargeVoltage != 4400 {
		t.Fatalf("%+v %v", v, err)
	}

	write(t, p, p.BatteryCapacityFile, []byte("15\n"))
	write(t, p, p.ChargerOnlineFiles[0], []byte("0\n"))
	var low *LowBatteryError
	if err := p.CheckPower(); !errors.As(err, &low) || low.Percent != 15 {
		t.Fatalf("15%% without a charger: %v", err)
	}
	write(t, p, p.ChargerOnlineFiles[0], []byte("1\n"))
	if err := p.CheckPower(); err != nil {
		t.Fatalf("15%% on the charger: %v", err)
	}
	write(t, p, p.ChargerOnlineFiles[0], []byte("0\n"))
	write(t, p, p.BatteryCapacityFile, []byte("80\n"))
	if err := p.CheckPower(); err != nil {
		t.Fatalf("80%%: %v", err)
	}
}

func TestLocations(t *testing.T) {
	p := testPlatform(t)
	os.MkdirAll(p.FSPath("/mnt/mmc"), 0o755) // TF1 only
	locs := p.Locations()
	if len(locs) != 2 || locs[0].ID != "tf2" || locs[0].Present || !locs[1].Present {
		t.Fatalf("locations %+v", locs)
	}
	if got := p.DefaultBackupDir(); got != p.FSPath("/mnt/mmc/rgdsplus-charge-tune/backups") {
		t.Fatalf("default %s", got)
	}
	dirs := p.BackupSearchDirs("/elsewhere")
	if len(dirs) != 3 || dirs[0] != "/elsewhere" {
		t.Fatalf("search dirs %v", dirs)
	}
	if got := p.DevicePath(p.FSPath("/mnt/mmc/x")); got != "/mnt/mmc/x" {
		t.Fatalf("DevicePath %s", got)
	}
}

func TestSystemLanguage(t *testing.T) {
	p := testPlatform(t)
	write(t, p, p.LanguageFile, []byte("6\n"))
	if got := p.SystemLanguage(); got != "ru" {
		t.Fatalf("language %q", got)
	}
	write(t, p, p.LanguageFile, []byte("42\n"))
	if got := p.SystemLanguage(); got != "" {
		t.Fatalf("out of range: %q", got)
	}
}

func TestLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	release, err := Lock(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Lock(path); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("second lock: %v", err)
	}
	release()
	again, err := Lock(path)
	if err != nil {
		t.Fatalf("after release: %v", err)
	}
	again()
}

func TestCheckBackupDir(t *testing.T) {
	p := testPlatform(t)
	card := p.FSPath("/mnt/sdcard")
	os.MkdirAll(card, 0o755)
	// Only the card is a filesystem of its own here.
	p.OnRootFS = func(path string) (bool, error) {
		return !strings.HasPrefix(path, card), nil
	}
	if err := p.CheckBackupDir(filepath.Join(card, "rgdsplus-charge-tune/backups")); err != nil {
		t.Fatalf("a folder still to be made on the card: %v", err)
	}
	if err := p.CheckBackupDir(p.FSPath("/userdata/backups")); !errors.Is(err, ErrNotOnCard) {
		t.Fatalf("a folder on the root filesystem: %v", err)
	}
	// Without the hook, development and -root runs skip the check.
	p.OnRootFS = nil
	if err := p.CheckBackupDir(p.FSPath("/userdata/backups")); err != nil {
		t.Fatalf("-root run: %v", err)
	}
}
