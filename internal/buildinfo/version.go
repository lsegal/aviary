// Package buildinfo holds build-time metadata injected by release builds.
//
//nolint:revive // buildinfo intentionally names the package after its purpose.
package buildinfo

// Version is injected at build time for release binaries.
// Local development builds fall back to "dev".
var Version = "dev"
