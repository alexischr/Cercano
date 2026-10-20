//go:build (!darwin && !linux && !windows) || (darwin && !cgo)

package launch

import "os"

func observeLifetime(*os.Process, func() error) error { return ErrObservation }
