// Package version holds the release version. `make bump` rewrites it; the
// build injects the tagged version via -ldflags -X.
package version

var Version = "0.6.3"
