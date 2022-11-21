package buildinfo

import "runtime/debug"

// DefaultVersion is the default version string if it's unset
const DefaultVersion = "UNKNOWN"

// FallbackVersion initializes the automatic version detection
func FallbackVersion(v *string, defaultv string) {
	if *v != defaultv {
		return
	}

	b, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}

	*v = b.Main.Version
}
