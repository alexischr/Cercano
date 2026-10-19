//go:build linux

package nativeprocess

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLinuxInspectCurrentProcess(t *testing.T) {
	id, e := Inspect(os.Getpid())
	if e != nil {
		t.Fatal(e)
	}
	executable, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	executable, e = filepath.EvalSymlinks(executable)
	if e != nil {
		t.Fatal(e)
	}
	if id.PID != os.Getpid() || id.UID != uint32(os.Geteuid()) || id.StartTicks == 0 || id.Executable != executable {
		t.Fatalf("bad kernel identity: %+v", id)
	}
	again, e := Inspect(os.Getpid())
	if e != nil || !id.SameProcess(again) {
		t.Fatal("unstable identity", again, e)
	}
}

func TestLinuxInspectRefusesInvalidOrAbsentProcess(t *testing.T) {
	for _, pid := range []int{0, -1, 2147483647} {
		if _, e := Inspect(pid); e == nil {
			t.Fatal("invalid/absent process accepted", pid)
		}
	}
}
