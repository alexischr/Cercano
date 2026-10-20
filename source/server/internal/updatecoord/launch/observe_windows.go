//go:build windows

package launch

import (
	"fmt"
	"golang.org/x/sys/windows"
	"os"
)

func observeLifetime(p *os.Process, observe func() error) error {
	var result error
	err := p.WithHandle(func(h uintptr) {
		alive := func() error {
			v, e := windows.WaitForSingleObject(windows.Handle(h), 0)
			if e != nil {
				return e
			}
			if v != uint32(windows.WAIT_TIMEOUT) {
				return ErrObservation
			}
			return nil
		}
		if result = alive(); result != nil {
			return
		}
		if result = observe(); result != nil {
			return
		}
		result = alive()
	})
	if err != nil {
		return fmt.Errorf("%w: %v", ErrObservation, err)
	}
	return result
}
