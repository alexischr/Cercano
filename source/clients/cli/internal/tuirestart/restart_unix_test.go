//go:build darwin || linux

package tuirestart

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println("cercano-cli fixture-1")
		os.Exit(0)
	}
	if len(os.Args) == 3 && os.Args[1] == "--version" && os.Args[2] == "tui-handoff-protocol" {
		fmt.Println(Protocol)
		os.Exit(0)
	}
	if len(os.Args) == 3 && os.Args[1] == "--internal-tui-handoff-fd" {
		n, e := strconv.Atoi(os.Args[2])
		if e != nil {
			os.Exit(3)
		}
		s, e := ReadInherited(n)
		if e != nil {
			os.Exit(4)
		}
		if e = json.NewEncoder(os.Stdout).Encode(s); e != nil {
			os.Exit(5)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}
func TestRestartExecHelper(t *testing.T) {
	if os.Getenv("CERCANO_TUI_RESTART_FIXTURE") != "yes" {
		return
	}
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	e = Replace(exe, State{ConversationID: "fixture-conversation", WorkingDirectory: os.Getenv("CERCANO_TUI_RESTART_DIR"), Draft: "unsent fixture\nα"})
	t.Fatal("replacement returned", e)
}
func TestOwnedProcessHandoff(t *testing.T) {
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "-test.run=^TestRestartExecHelper$")
	cmd.Env = append(os.Environ(), "CERCANO_TUI_RESTART_FIXTURE=yes", "CERCANO_TUI_RESTART_DIR="+dir)
	b, e := cmd.Output()
	if e != nil {
		t.Fatal("owned replacement", e)
	}
	var got State
	if e = json.Unmarshal(b, &got); e != nil {
		t.Fatal(e, string(b))
	}
	if got.ConversationID != "fixture-conversation" || got.WorkingDirectory != dir || got.Draft != "unsent fixture\nα" {
		t.Fatal("handoff lost state", got)
	}
}
func TestUnsupportedOldClientDoesNotLaunch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "old-client")
	marker := filepath.Join(dir, "started")
	script := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo cercano-cli-old; exit 0; fi\ntouch '" + marker + "'\nexit 9\n"
	if e := os.WriteFile(path, []byte(script), 0700); e != nil {
		t.Fatal(e)
	}
	if e := Replace(path, State{}); e == nil {
		t.Fatal("unsupported client accepted")
	}
	if _, e := os.Stat(marker); !os.IsNotExist(e) {
		t.Fatal("old client was started", e)
	}
}
func TestInheritedStandardStreamsRefused(t *testing.T) {
	for _, fd := range []int{-1, 0, 1, 2} {
		if _, e := ReadInherited(fd); e == nil {
			t.Fatal("standard stream accepted")
		}
	}
}

func TestRestartRequiresAnnouncedInstalledVersion(t *testing.T) {
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	if e = CheckTarget(exe, "fixture-1"); e != nil {
		t.Fatal(e)
	}
	if e = CheckTarget(exe, "fixture-2"); e == nil {
		t.Fatal("stale installed target accepted")
	}
}
