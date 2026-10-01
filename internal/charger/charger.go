// Package charger knows the RK817 charger settings: the current steps the
// Rockchip BSP driver (rk817_charger) can program, the modes the app offers
// and the values the running kernel was booted with.
package charger

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Driver steps. The driver rounds a device tree value down to one of these
// (CHRG_CUR_SEL in register 0xE4 and USB_ILIM_SEL in 0xE5, 10 mOhm sense
// resistor). The chip also has a 2750 mA charge step that the driver never
// selects, so it is not offered.
var (
	ChargeSteps = []int{500, 1000, 1500, 2000, 2500, 3000, 3500}
	InputSteps  = []int{80, 450, 850, 1500, 1750, 2000, 2500, 3000}
)

// Limits the app allows whatever a configuration says. Above 2500 mA the
// battery (4247 mAh, no temperature sensor in the device tree) would charge
// at more than 0.6C with nothing watching its temperature.
const (
	MaxChargeCurrent = 2500
	MinChargeCurrent = 500
	MaxInputCurrent  = 3000
	MinInputCurrent  = 450
)

// DriverCharge is the charge current the driver programs for a device tree
// value (mA): the largest step not above it, at least the smallest step.
func DriverCharge(mA int) int { return roundDown(ChargeSteps, mA) }

// DriverInput is the input current limit the driver programs for a device
// tree value when a charger (DCP) is connected.
func DriverInput(mA int) int { return roundDown(InputSteps, mA) }

func roundDown(steps []int, v int) int {
	out := steps[0]
	for _, s := range steps {
		if v >= s {
			out = s
		}
	}
	return out
}

// Mode is a pair of limits the user can choose.
type Mode struct {
	ID            string `json:"id"`
	ChargeCurrent int    `json:"charge_ma"`
	InputCurrent  int    `json:"input_ma"`
	Recommended   bool   `json:"recommended,omitempty"`
	// Warning marks modes whose confirmation shows the battery warning.
	Warning bool `json:"warning,omitempty"`
}

// Validate checks that a mode uses exact driver steps within the app's
// limits.
func (m Mode) Validate() error {
	if m.ID == "" {
		return errors.New("mode without an id")
	}
	if DriverCharge(m.ChargeCurrent) != m.ChargeCurrent || m.ChargeCurrent < MinChargeCurrent || m.ChargeCurrent > MaxChargeCurrent {
		return fmt.Errorf("mode %s: charge current %d mA is not one of %v within %d-%d", m.ID, m.ChargeCurrent, ChargeSteps, MinChargeCurrent, MaxChargeCurrent)
	}
	if DriverInput(m.InputCurrent) != m.InputCurrent || m.InputCurrent < MinInputCurrent || m.InputCurrent > MaxInputCurrent {
		return fmt.Errorf("mode %s: input current %d mA is not one of %v within %d-%d", m.ID, m.InputCurrent, InputSteps, MinInputCurrent, MaxInputCurrent)
	}
	return nil
}

// ValidModes drops invalid or duplicate modes (reporting them) so that a
// bad platform.json cannot offer an unsafe value.
func ValidModes(modes []Mode) (out []Mode, problems []error) {
	seen := map[string]bool{}
	for _, m := range modes {
		if err := m.Validate(); err != nil {
			problems = append(problems, err)
			continue
		}
		if seen[m.ID] {
			problems = append(problems, fmt.Errorf("mode %s listed twice", m.ID))
			continue
		}
		seen[m.ID] = true
		out = append(out, m)
	}
	return out, problems
}

// Values are charger settings from a device tree (mA, mV).
type Values struct {
	ChargeCurrent int `json:"charge_ma"`
	InputCurrent  int `json:"input_ma"`
	ChargeVoltage int `json:"voltage_mv"`
}

// Same reports whether two value sets program the same currents.
func (v Values) Same(o Values) bool {
	return v.ChargeCurrent == o.ChargeCurrent && v.InputCurrent == o.InputCurrent
}

// Matches reports whether the values are those of the mode.
func (m Mode) Matches(v Values) bool {
	return v.ChargeCurrent == m.ChargeCurrent && v.InputCurrent == m.InputCurrent
}

// Find returns the mode with the given values.
func Find(modes []Mode, v Values) (Mode, bool) {
	for _, m := range modes {
		if m.Matches(v) {
			return m, true
		}
	}
	return Mode{}, false
}

// Device tree property names (see bootimg).
const (
	propChargeCurrent = "max_chrg_current"
	propInputCurrent  = "max_input_current"
	propChargeVoltage = "max_chrg_voltage"
	compatible        = "rk817,charger"
)

// ReadRunning reads the values the running kernel was booted with from the
// live device tree (/proc/device-tree): nodePath first, else the node whose
// compatible is rk817,charger.
func ReadRunning(dtRoot, nodePath string) (Values, string, error) {
	dir := filepath.Join(dtRoot, filepath.FromSlash(strings.TrimPrefix(nodePath, "/")))
	if nodePath == "" || !isCharger(dir) {
		found, err := findCharger(dtRoot)
		if err != nil {
			return Values{}, "", err
		}
		dir = found
	}
	var v Values
	for _, f := range []struct {
		name string
		dst  *int
	}{{propChargeCurrent, &v.ChargeCurrent}, {propInputCurrent, &v.InputCurrent}, {propChargeVoltage, &v.ChargeVoltage}} {
		data, err := os.ReadFile(filepath.Join(dir, f.name))
		if err != nil {
			return Values{}, dir, err
		}
		if len(data) != 4 {
			return Values{}, dir, fmt.Errorf("%s: %d bytes, not one cell", f.name, len(data))
		}
		*f.dst = int(binary.BigEndian.Uint32(data))
	}
	return v, dir, nil
}

// ErrNoChargerNode means the live device tree has no RK817 charger.
var ErrNoChargerNode = errors.New("no " + compatible + " node in the device tree")

func isCharger(dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "compatible"))
	if err != nil {
		return false
	}
	for _, s := range strings.Split(strings.TrimRight(string(data), "\x00"), "\x00") {
		if s == compatible {
			return true
		}
	}
	return false
}

// findCharger searches the live device tree for the charger node. The
// tree is small (a few thousand nodes) and this runs once.
func findCharger(root string) (string, error) {
	var found string
	errStop := errors.New("stop")
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable subtree: skip it
		}
		if d.IsDir() && isCharger(p) {
			found = p
			return errStop
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStop) {
		return "", err
	}
	if found == "" {
		return "", ErrNoChargerNode
	}
	return found, nil
}
