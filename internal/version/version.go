// Package version holds build metadata injected via -ldflags.
package version

var (
	Version   = "dev"
	GitCommit = "unknown"
	BuildDate = "unknown"
)

// Homepage is the project page shown in About.
const Homepage = "https://github.com/Misaka0x2730/rgdsplus-charge-tune"

// String returns a one-line human readable version.
func String() string {
	return Version + " (" + GitCommit + ", " + BuildDate + ")"
}
