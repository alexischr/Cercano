//go:build !darwin || !cgo

package brewrestart

import (
	"fmt"
)

func CaptureLaunchState(expected Identity) (LaunchState, error) {
	return LaunchState{}, fmt.Errorf("launch-state capture requires macOS with cgo")
}
