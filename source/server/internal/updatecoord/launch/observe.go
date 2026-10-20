package launch

import (
	"errors"
	"fmt"
	"os"
)

var ErrObservation = errors.New("launch: owned child identity observation unavailable")

func observeOwned(p *os.Process, observe func() error) (err error) {
	defer func() {
		if recover() != nil {
			err = fmt.Errorf("%w: observer panicked", ErrObservation)
		}
	}()
	return observeLifetime(p, observe)
}
