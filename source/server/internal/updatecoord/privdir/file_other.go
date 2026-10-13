//go:build !darwin && !linux && !windows

package privdir

import "os"

func CreatePrivateFile(string) (*os.File, error) { return nil, ErrUnsupportedPlatform }
func OpenPrivateFile(string) (*os.File, error)   { return nil, ErrUnsupportedPlatform }
