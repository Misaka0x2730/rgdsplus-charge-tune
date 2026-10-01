package charger

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// The thresholds come from the driver's code in the 20260915 kernel: each
// value between two steps programs the lower one.
func TestDriverRounding(t *testing.T) {
	charge := map[int]int{0: 500, 499: 500, 999: 500, 1000: 1000, 1499: 1000, 2000: 2000, 2499: 2000,
		2500: 2500, 2750: 2500, 2999: 2500, 3000: 3000, 3499: 3000, 3500: 3500, 9999: 3500}
	for in, want := range charge {
		if got := DriverCharge(in); got != want {
			t.Errorf("charge %d -> %d, want %d", in, got, want)
		}
	}
	input := map[int]int{0: 80, 449: 80, 450: 450, 849: 450, 850: 850, 1499: 850, 1500: 1500,
		1749: 1500, 1750: 1750, 2000: 2000, 2499: 2000, 2500: 2500, 3000: 3000, 5000: 3000}
	for in, want := range input {
		if got := DriverInput(in); got != want {
			t.Errorf("input %d -> %d, want %d", in, got, want)
		}
	}
}

func TestValidModes(t *testing.T) {
	modes := []Mode{
		{ID: "stock", ChargeCurrent: 2000, InputCurrent: 1500},
		{ID: "fast", ChargeCurrent: 2500, InputCurrent: 3000},
		{ID: "hot", ChargeCurrent: 3000, InputCurrent: 3000},    // above the charge limit
		{ID: "odd", ChargeCurrent: 2750, InputCurrent: 3000},    // not a driver step
		{ID: "pc", ChargeCurrent: 1000, InputCurrent: 80},       // below the input limit
		{ID: "stock", ChargeCurrent: 1000, InputCurrent: 2000},  // duplicate id
		{ID: "", ChargeCurrent: 1000, InputCurrent: 2000},       // no id
		{ID: "gentle", ChargeCurrent: 1000, InputCurrent: 2000}, // fine
	}
	ok, problems := ValidModes(modes)
	if len(ok) != 3 || ok[0].ID != "stock" || ok[1].ID != "fast" || ok[2].ID != "gentle" {
		t.Fatalf("valid modes %+v", ok)
	}
	if len(problems) != 5 {
		t.Fatalf("%d problems: %v", len(problems), problems)
	}
	if m, found := Find(ok, Values{ChargeCurrent: 2500, InputCurrent: 3000, ChargeVoltage: 4400}); !found || m.ID != "fast" {
		t.Fatalf("Find = %+v, %v", m, found)
	}
}

func writeCell(t *testing.T, path string, v uint32) {
	t.Helper()
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func fakeTree(t *testing.T, nodePath string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, filepath.FromSlash(nodePath))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "compatible"), []byte("rk817,charger\x00"), 0o644)
	writeCell(t, filepath.Join(dir, "max_chrg_current"), 2500)
	writeCell(t, filepath.Join(dir, "max_input_current"), 3000)
	writeCell(t, filepath.Join(dir, "max_chrg_voltage"), 4400)
	return root
}

func TestReadRunning(t *testing.T) {
	root := fakeTree(t, "i2c@fdd40000/pmic@20/charger")
	want := Values{ChargeCurrent: 2500, InputCurrent: 3000, ChargeVoltage: 4400}
	v, _, err := ReadRunning(root, "/i2c@fdd40000/pmic@20/charger")
	if err != nil || v != want {
		t.Fatalf("configured path: %+v, %v", v, err)
	}
	// Another firmware moved the node: found by its compatible.
	moved := fakeTree(t, "i2c@fe5a0000/pmic@20/charger")
	v, dir, err := ReadRunning(moved, "/i2c@fdd40000/pmic@20/charger")
	if err != nil || v != want || filepath.Base(filepath.Dir(filepath.Dir(dir))) != "i2c@fe5a0000" {
		t.Fatalf("search: %+v, %s, %v", v, dir, err)
	}
	if _, _, err := ReadRunning(t.TempDir(), "/i2c@fdd40000/pmic@20/charger"); err == nil {
		t.Fatal("no charger found in an empty tree")
	}
}
