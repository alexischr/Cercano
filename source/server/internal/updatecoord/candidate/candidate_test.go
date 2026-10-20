//go:build linux || windows || (darwin && cgo)

package candidate

import (
	"cercano/source/server/internal/updatecoord/launch"
	"cercano/source/server/internal/updatecoord/nativeprocess"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCandidateChild(t *testing.T) {
	if os.Getenv("CERCANO_CANDIDATE_FIXTURE") != "1" {
		return
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, e := os.Stat(os.Getenv("CERCANO_CANDIDATE_RELEASE")); e == nil {
			os.Exit(0)
		}
		time.Sleep(5 * time.Millisecond)
	}
	os.Exit(2)
}
func options(t *testing.T) (launch.Options, func()) {
	t.Helper()
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	release := filepath.Join(t.TempDir(), "release")
	return launch.Options{Executable: exe, Argv: []string{"-test.run=^TestCandidateChild$"}, Env: append(os.Environ(), "CERCANO_CANDIDATE_FIXTURE=1", "CERCANO_CANDIDATE_RELEASE="+release)}, func() {
		if e := os.WriteFile(release, []byte("done"), 0600); e != nil {
			t.Error(e)
		}
	}
}
func complete(t *testing.T, c *Candidate, release func()) {
	t.Helper()
	release()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	proof, e := c.Process().WaitForCompletion(ctx)
	if e != nil || !proof.For(c.Process()) {
		t.Fatal(e)
	}
	if _, e = c.Identity(); !errors.Is(e, ErrIdentity) {
		t.Fatal("completed candidate retained identity", e)
	}
}
func TestCandidateExactOwnedLifetime(t *testing.T) {
	if candidateTestHost(t) {
		return
	}
	o, release := options(t)
	c, e := Start(o)
	if c != nil {
		defer complete(t, c, release)
	}
	if e != nil {
		t.Fatal(e)
	}
	id, e := c.Identity()
	if e != nil || id.PID != c.Process().Pid() {
		t.Fatal(id, e)
	}
}
func TestCandidateInspectionFailureRetainsCompletionOnly(t *testing.T) {
	if candidateTestHost(t) {
		return
	}
	for _, kind := range []string{"failure", "wrong-pid", "changed-lifetime", "wrong-image", "panic"} {
		t.Run(kind, func(t *testing.T) {
			o, release := options(t)
			calls := 0
			c, e := start(o, func(pid int) (nativeprocess.Identity, error) {
				calls++
				if kind == "failure" {
					return nativeprocess.Identity{}, errors.New("unavailable")
				}
				if kind == "panic" {
					panic("fixture")
				}
				id, e := nativeprocess.Inspect(pid)
				if kind == "wrong-pid" {
					id.PID++
				}
				if kind == "wrong-image" {
					id.Executable = os.DevNull
				}
				if kind == "changed-lifetime" && calls == 2 {
					id.StartTicks++
				}
				return id, e
			})
			if c != nil {
				defer complete(t, c, release)
			}
			if c == nil || !errors.Is(e, ErrIdentity) {
				t.Fatal(c, e)
			}
			if _, e = c.Identity(); !errors.Is(e, ErrIdentity) {
				t.Fatal("failed capture authorized", e)
			}
		})
	}
}
func TestCandidatePrestartRefusal(t *testing.T) {
	c, e := Start(launch.Options{Executable: "relative"})
	if c != nil || e == nil {
		t.Fatal(c, e)
	}
	var zero Candidate
	if _, e := zero.Identity(); !errors.Is(e, ErrIdentity) {
		t.Fatal(e)
	}
}
