//go:build darwin || linux

package stablelaunch

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestStableLauncherPreservesExplicitInheritedDescriptor(t *testing.T) {
	f := fixture(t)
	file, err := os.CreateTemp(t.TempDir(), "handoff")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err = file.WriteString("private handoff"); err != nil {
		t.Fatal(err)
	}
	if _, err = file.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(f.Root, f.ClientName))
	cmd.Env = append(f.ChildEnv(), helperEnv+"=1", "CERCANO_STABLE_ROLE=client", "CERCANO_STABLE_FD=3")
	cmd.ExtraFiles = []*os.File{file}
	out, err := cmd.CombinedOutput()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 17 {
		t.Fatalf("inherited descriptor lost: %v %s", err, out)
	}
}
