package forcesave

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDecideRefusesBeforeOpeningState(t *testing.T) {
	root := filepath.Join(t.TempDir(), "must-not-create")
	l := &Local{launcher: filepath.Join(root, "cercano-cli"), root: root}
	for _, tc := range []struct {
		r              Receipt
		force, confirm bool
	}{
		{Receipt{ID: "x", Expires: time.Now().Add(time.Minute)}, true, false},
		{Receipt{ID: "x", Expires: time.Now().Add(-time.Second)}, true, true},
		{Receipt{Expires: time.Now().Add(time.Minute)}, false, false},
	} {
		if e := l.Decide(context.Background(), tc.r, tc.force, tc.confirm); e != ErrUnavailable {
			t.Fatal(e)
		}
	}
	if _, e := os.Stat(root); !os.IsNotExist(e) {
		t.Fatal("refusal touched state", e)
	}
}
func TestUnenrolledLocalCannotReadOrSubmit(t *testing.T) {
	root := t.TempDir()
	launcher := filepath.Join(root, "cercano-cli")
	if e := os.WriteFile(launcher, []byte("fixture"), 0700); e != nil {
		t.Fatal(e)
	}
	l := &Local{launcher: launcher, root: filepath.Join(root, "state")}
	if _, ok, e := l.Pending(context.Background()); e == nil || ok {
		t.Fatal("unenrolled reader authorized", ok, e)
	}
	if e := l.Decide(context.Background(), Receipt{ID: "x", Expires: time.Now().Add(time.Minute)}, true, true); e == nil {
		t.Fatal("unenrolled writer authorized")
	}
	if _, e := os.Stat(l.root); !os.IsNotExist(e) {
		t.Fatal("created state", e)
	}
}
