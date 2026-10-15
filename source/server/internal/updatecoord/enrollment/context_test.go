package enrollment

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChildContextRequiresIndependentImageBinding(t *testing.T) {
	root := fixture(t)
	record := created(t, root)
	a, _ := names()
	dir := filepath.Join(root, "versions", "v1")
	if e := os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	exe := filepath.Join(dir, a)
	payload := []byte("inert versioned image")
	if e := os.WriteFile(exe, payload, 0700); e != nil {
		t.Fatal(e)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(payload))
	handoff := ChildContext{root, record.InstallID}
	got, e := VerifyChildContext(context.Background(), handoff, exe, exe, digest, 1024)
	if e != nil || got != record {
		t.Fatal(got, e)
	}
	// Existing adjacent-only discovery must not silently adopt versioned children.
	if _, e := DiscoverStableLauncher(context.Background(), exe); e == nil {
		t.Fatal("implicit ancestor adoption")
	}
	cases := []struct {
		name                   string
		h                      ChildContext
		actual, selected, hash string
		bound                  int64
	}{
		{"wrong-install", ChildContext{root, strings.Repeat("0", 32)}, exe, exe, digest, 1024},
		{"missing-context", ChildContext{}, exe, exe, digest, 1024},
		{"different-image", handoff, exe, filepath.Join(root, a), digest, 1024},
		{"wrong-digest", handoff, exe, exe, strings.Repeat("0", 64), 1024},
		{"relative", handoff, a, a, digest, 1024},
		{"oversize", handoff, exe, exe, digest, 1},
		{"unbounded", handoff, exe, exe, digest, 1 << 31},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, e := VerifyChildContext(context.Background(), tc.h, tc.actual, tc.selected, tc.hash, tc.bound); e == nil {
				t.Fatal("unsafe context accepted")
			}
		})
	}
	outside := filepath.Join(t.TempDir(), a)
	if e := os.WriteFile(outside, payload, 0700); e != nil {
		t.Fatal(e)
	}
	if _, e := VerifyChildContext(context.Background(), handoff, outside, outside, digest, 1024); e == nil {
		t.Fatal("outside image accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := VerifyChildContext(ctx, handoff, exe, exe, digest, 1024); e != context.Canceled {
		t.Fatal(e)
	}
	if e := os.WriteFile(exe, []byte("changed image"), 0700); e != nil {
		t.Fatal(e)
	}
	if _, e := VerifyChildContext(context.Background(), handoff, exe, exe, digest, 1024); e == nil {
		t.Fatal("modified image accepted")
	}
}
