//go:build !windows

package candidate

import "testing"

func candidateTestHost(*testing.T) bool { return false }
