// Package version is set at build time via -ldflags.
package version

var (
	Version = "dev"
	Commit  = "unknown"
	Date    = ""
)
