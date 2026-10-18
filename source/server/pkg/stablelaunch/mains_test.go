package stablelaunch

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"cercano/source/server/pkg/installcontexttest"
)

// Build real mains and invoke only their offline version commands through an
// enrolled temporary stable path. Never launch an agent, TUI or live state.
func TestStableLauncherRealMainsOfflineVersion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	dir := t.TempDir()
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	agent, client := filepath.Join(dir, "agent"+suffix), filepath.Join(dir, "client"+suffix)
	for _, item := range []struct{ output, dir, pkg string }{{agent, ".", "../../cmd/cercano"}, {client, "../../../clients/cli", "."}} {
		cmd := exec.CommandContext(ctx, "go", "build", "-o", item.output, item.pkg)
		cmd.Dir = item.dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build real entrypoint: %v %s", err, out)
		}
	}
	f := installcontexttest.Build(t, agent, client)
	for _, item := range []struct{ source, name, arg, prefix string }{{agent, f.AgentName, "version", "cercano v"}, {client, f.ClientName, "--version", "cercano-cli v"}} {
		data, err := os.ReadFile(item.source)
		if err != nil {
			t.Fatal(err)
		}
		stable := filepath.Join(f.Root, item.name)
		if err = os.WriteFile(stable, data, 0700); err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(ctx, stable, item.arg)
		cmd.Env = f.ChildEnv()
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("installed %s: %v %s", item.name, err, out)
		}
		if !strings.HasPrefix(string(out), item.prefix) {
			t.Fatalf("wrong offline output: %s", out)
		}
		// SQLite coordination files may be used by read-only observation; fixture
		// do not count as normal app initialization. Configuration must stay absent.
		if _, err = os.Stat(filepath.Join(f.Home, ".config", "cercano", "config.yaml")); !os.IsNotExist(err) {
			t.Fatalf("version initialized app config: %v", err)
		}
	}
}
