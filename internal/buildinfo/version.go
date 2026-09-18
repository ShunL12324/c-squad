// Package buildinfo exposes metadata injected by the release build.
// Development builds remain usable without Git or linker overrides.
package buildinfo

import "fmt"

// Build metadata defaults identify a local development binary. GoReleaser sets
// these strings with linker flags; they must remain variables rather than constants.
var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

// String returns the version and source identity shown by csquad version.
func String() string { return fmt.Sprintf("csquad %s (commit %s, built %s)", Version, Commit, Date) }
