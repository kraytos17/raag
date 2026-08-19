// Package version holds build-time version metadata. Both binaries inject it
// via -ldflags -X; when unset it reports "dev" / "unknown".
package version

import "fmt"

var (
	// Version is the semantic version, set at build time (e.g. v0.1.0).
	Version = "dev"
	// BuildTime is the RFC3339 build timestamp, set at build time.
	BuildTime = "unknown"
)

// String renders the full version line used by `raag --version` and
// `raagd -version`.
func String(name string) string {
	return fmt.Sprintf("%s %s (%s)", name, Version, BuildTime)
}
