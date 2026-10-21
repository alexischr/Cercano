// Package buildversion formats linker-supplied versions for display.
package buildversion

import "strings"

// Display keeps the unversioned development fallback honest and prefixes
// release versions with exactly one v.
func Display(version string) string {
	version = strings.TrimPrefix(version, "v")
	if version == "dev" {
		return version
	}
	return "v" + version
}
