package enrollment

import (
	"context"
	"os"
	"path/filepath"
)

// DiscoverStableLauncher reads the record immediately beside an explicit stable
// launcher. It never searches ancestors, derives an ID from a pathname, scans a
// registry or enrolls. A versioned child executable is not a stable launcher;
// its parent bootstrap must pass the installation context explicitly instead.
// Package-manager and machine-scope authority remain outside this per-user API.
func DiscoverStableLauncher(ctx context.Context, launcher string) (Record, error) {
	if !filepath.IsAbs(launcher) || filepath.Clean(launcher) != launcher {
		return Record{}, ErrUnsafe
	}
	a, c := names()
	base := filepath.Base(launcher)
	if base != a && base != c {
		return Record{}, ErrUnsafe
	}
	named, e := os.Lstat(launcher)
	if e != nil || !named.Mode().IsRegular() {
		return Record{}, ErrUnsafe
	}
	return Read(ctx, filepath.Dir(launcher))
}
