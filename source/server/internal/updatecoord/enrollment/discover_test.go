package enrollment

import (
	"cercano/source/server/internal/updatecoord/privdir"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoveryRequiresAdjacentEnrolledStableLauncher(t *testing.T) {
	root := fixture(t)
	a, c := names()
	ctx := context.Background()
	if _, e := DiscoverStableLauncher(ctx, filepath.Join(root, a)); !os.IsNotExist(e) {
		t.Fatal("unregistered launcher adopted", e)
	}
	wanted := created(t, root)
	for _, name := range []string{a, c} {
		got, e := DiscoverStableLauncher(ctx, filepath.Join(root, name))
		if e != nil || got != wanted {
			t.Fatal("stable launcher not bound", got, e)
		}
	}
	// No ancestor lookup that could accidentally bind a versioned executable or
	// copied development binary to the installation above it.
	child := filepath.Join(root, "child")
	if _, e := privdir.Ensure(child); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{a, c} {
		if e := os.WriteFile(filepath.Join(child, name), []byte("inert"), 0700); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := DiscoverStableLauncher(ctx, filepath.Join(child, a)); !os.IsNotExist(e) {
		t.Fatal("ancestor record used", e)
	}
	if _, e := os.Stat(filepath.Join(child, FileName)); !os.IsNotExist(e) {
		t.Fatal("child enrolled")
	}
}
