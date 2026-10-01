package gabagool

import (
	"time"

	"github.com/BrandonKowalski/gabagool/v2/pkg/gabagool/constants"
	"github.com/BrandonKowalski/gabagool/v2/pkg/gabagool/internal"
)

// ComboType distinguishes between chord and sequence combinations
type ComboType = internal.ComboType

const (
	// ComboTypeChord represents buttons pressed simultaneously
	ComboTypeChord = internal.ComboTypeChord
	// ComboTypeSequence represents buttons pressed in order
	ComboTypeSequence = internal.ComboTypeSequence
)

// ComboEvent represents a triggered button combination
type ComboEvent = internal.ComboEvent

// ComboCallback is called when a combo is triggered or released
type ComboCallback = internal.ComboCallback

// ChordOptions configures chord detection behavior
type ChordOptions struct {
	// Window is the time window for simultaneous press (default: 100ms)
	Window time.Duration
	// OnTrigger is called when the chord is activated (all buttons pressed)
	OnTrigger ComboCallback
	// OnRelease is called when the chord is released (any button released)
	OnRelease ComboCallback
}

// SequenceOptions configures sequence detection behavior
type SequenceOptions struct {
	// Timeout is the max time between sequence inputs (default: 500ms)
	Timeout time.Duration
	// Strict ensures no other buttons are pressed during the sequence
	Strict bool
	// OnTrigger is called when the sequence is completed
	OnTrigger ComboCallback
}

// RegisterChord registers a chord combination (multiple buttons pressed simultaneously).
// Returns an error if fewer than 2 buttons are provided.
//
// Example:
//
//	gabagool.RegisterChord("quick_menu", []constants.VirtualButton{
//	    constants.VirtualButtonL1,
//	    constants.VirtualButtonR1,
//	}, gabagool.ChordOptions{
//	    Window: 150 * time.Millisecond,
//	    OnTrigger: func() {
//	        fmt.Println("Quick menu activated!")
//	    },
//	})
func RegisterChord(id string, buttons []constants.VirtualButton, opts ChordOptions) error {
	return internal.GetInputProcessor().RegisterChord(id, buttons, internal.ChordOptions{
		Window:    opts.Window,
		OnTrigger: opts.OnTrigger,
		OnRelease: opts.OnRelease,
	})
}

// RegisterSequence registers a sequence combination (buttons pressed in order).
// Returns an error if fewer than 2 buttons are provided.
//
// Example:
//
//	gabagool.RegisterSequence("konami", []constants.VirtualButton{
//	    constants.VirtualButtonUp,
//	    constants.VirtualButtonUp,
//	    constants.VirtualButtonDown,
//	    constants.VirtualButtonDown,
//	}, gabagool.SequenceOptions{
//	    Timeout: 500 * time.Millisecond,
//	    OnTrigger: func() {
//	        fmt.Println("Konami code entered!")
//	    },
//	})
func RegisterSequence(id string, buttons []constants.VirtualButton, opts SequenceOptions) error {
	return internal.GetInputProcessor().RegisterSequence(id, buttons, internal.SequenceOptions{
		Timeout:   opts.Timeout,
		Strict:    opts.Strict,
		OnTrigger: opts.OnTrigger,
	})
}

// UnregisterCombo removes a previously registered combo by its ID
func UnregisterCombo(id string) {
	internal.GetInputProcessor().UnregisterCombo(id)
}

// ClearCombos removes all registered combos
func ClearCombos() {
	internal.GetInputProcessor().ClearCombos()
}

// ProcessComboEvent returns the next queued combo event, or nil if none are pending.
// Note: If you're using callbacks (OnTrigger/OnRelease), you typically don't need
// to call this function as the callbacks are invoked automatically.
func ProcessComboEvent() *ComboEvent {
	return internal.GetInputProcessor().ProcessComboEvent()
}
