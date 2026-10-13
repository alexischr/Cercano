package privdir

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPrivateFileCreationAndReadback(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "private")
	if _, e := Ensure(dir); e != nil {
		t.Fatal(e)
	}
	p := filepath.Join(dir, "identity")
	f, e := CreatePrivateFile(p)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.Write([]byte("fixture")); e != nil {
		f.Close()
		t.Fatal(e)
	}
	if e = f.Close(); e != nil {
		t.Fatal(e)
	}
	if f, e = CreatePrivateFile(p); e == nil {
		f.Close()
		t.Fatal("overwrote private file")
	}
	f, e = OpenPrivateFile(p)
	if e != nil {
		t.Fatal(e)
	}
	f.Close()
	if runtime.GOOS != "windows" {
		if e = os.Chmod(p, 0666); e != nil {
			t.Fatal(e)
		}
		if f, e = OpenPrivateFile(p); e == nil {
			f.Close()
			t.Fatal("unsafe mode accepted")
		}
		info, e := os.Stat(p)
		if e != nil || info.Mode().Perm() != 0666 {
			t.Fatal("unsafe file was repaired", e)
		}
	}
	if _, e = OpenPrivateFile(dir); e == nil {
		t.Fatal("directory accepted as file")
	}
}
