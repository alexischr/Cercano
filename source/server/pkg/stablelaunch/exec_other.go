//go:build !darwin && !linux && !windows

package stablelaunch

import "errors"

func execute(*prepared) (int, error) { return 1, errors.New("stablelaunch: unsupported platform") }
