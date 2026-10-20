//go:build linux || windows || (darwin && cgo)

package launch

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestObservationChild(t *testing.T) {
	if os.Getenv("CERCANO_OBSERVE_FIXTURE") != "1" {
		return
	}
	release := os.Getenv("CERCANO_OBSERVE_RELEASE")
	if release != "" {
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			if _, e := os.Stat(release); e == nil {
				os.Exit(0)
			}
			time.Sleep(5 * time.Millisecond)
		}
		os.Exit(2)
	}
	time.Sleep(300 * time.Millisecond)
	os.Exit(0)
}
func observationChild(t *testing.T) *exec.Cmd {
	t.Helper()
	c := exec.Command(os.Args[0], "-test.run=^TestObservationChild$")
	c.Env = append(os.Environ(), "CERCANO_OBSERVE_FIXTURE=1", "CERCANO_OBSERVE_RELEASE="+filepath.Join(t.TempDir(), "exit"))
	if e := c.Start(); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = c.Process.Kill(); _ = c.Wait() })
	return c
}
func TestOwnedObservationLivingChild(t *testing.T) {
	c := observationChild(t)
	called := false
	if e := observeOwned(c.Process, func() error { called = true; return nil }); e != nil {
		t.Fatal(e)
	}
	if !called {
		t.Fatal("observer not called")
	}
}
func TestOwnedObservationRejectsExitedChild(t *testing.T) {
	c := observationChild(t)
	e := observeOwned(c.Process, func() error {
		releaseObservationChild(t, c)
		// The race runtime may delay process exit. Observe actual termination,
		// without reaping, rather than assuming a sleep proves it.
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if observeLifetime(c.Process, func() error { return nil }) != nil {
				return nil
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Error("fixture did not exit")
		return nil
	})
	if e == nil {
		t.Fatal("exited child accepted")
	}
}
func TestOwnedObservationRejectsReapedChild(t *testing.T) {
	c := observationChild(t)
	releaseObservationChild(t, c)
	if e := c.Wait(); e != nil {
		t.Fatal(e)
	}
	called := false
	if e := observeOwned(c.Process, func() error { called = true; return nil }); e == nil || called {
		t.Fatal("reaped child observed", e, called)
	}
}
func TestOwnedObservationPreservesFailureAndPanic(t *testing.T) {
	for _, panics := range []bool{false, true} {
		c := observationChild(t)
		sentinel := errors.New("probe refused")
		e := observeOwned(c.Process, func() error {
			if panics {
				panic("test")
			}
			return sentinel
		})
		if panics && !errors.Is(e, ErrObservation) || !panics && !errors.Is(e, sentinel) {
			t.Fatal(e)
		}
	}
}

// Generic Launch behavior remains unchanged. Post-start observation failures
// still expose the same completion receipt; never signal from production.
func TestObservedLaunchFailureRetainsCompletion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("detached breakaway tested by existing owned Windows job fixtures")
	}
	sentinel := errors.New("inspection refused")
	p, e := LaunchObserved(Options{Executable: os.Args[0], Argv: []string{"-test.run=^TestObservationChild$"}, Env: append(os.Environ(), "CERCANO_OBSERVE_FIXTURE=1")}, func(int) error { return sentinel })
	if p == nil || !errors.Is(e, sentinel) {
		t.Fatal(p, e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	proof, e := p.WaitForCompletion(ctx)
	if e != nil || !proof.For(p) {
		t.Fatal(e)
	}
}

func releaseObservationChild(t *testing.T, c *exec.Cmd) {
	t.Helper()
	for _, v := range c.Env {
		if strings.HasPrefix(v, "CERCANO_OBSERVE_RELEASE=") {
			if e := os.WriteFile(strings.TrimPrefix(v, "CERCANO_OBSERVE_RELEASE="), []byte("exit"), 0600); e != nil {
				t.Fatal(e)
			}
			return
		}
	}
	t.Fatal("release fixture missing")
}
