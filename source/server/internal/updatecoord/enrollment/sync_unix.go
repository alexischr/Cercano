//go:build darwin || linux

package enrollment

import "os"

func syncIdentityDirectory(f *os.File) error { return f.Sync() }
