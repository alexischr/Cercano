//go:build !darwin && !linux

package enrollment

import (
	"errors"
	"os"
)

// File contents are flushed before publication. Directory-entry durability is
// not proven on these platforms; report publication uncertainty, never erase it.
func syncIdentityDirectory(*os.File) error {
	return errors.New("directory-entry durability is unverified on this platform")
}
