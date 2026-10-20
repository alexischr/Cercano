//go:build linux

package launch

import (
	"fmt"
	"golang.org/x/sys/unix"
	"os"
)

func observeLifetime(p *os.Process, observe func() error) error {
	var result error
	err := p.WithHandle(func(h uintptr) {
		alive := func() error {
			fds := []unix.PollFd{{Fd: int32(h), Events: unix.POLLIN}}
			n, e := unix.Poll(fds, 0)
			if e != nil {
				return e
			}
			if n != 0 {
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
