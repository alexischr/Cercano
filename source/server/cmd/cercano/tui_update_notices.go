package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"runtime"

	"cercano/source/server/internal/server"
	"cercano/source/server/internal/updatecoord/enrollment"
	"cercano/source/server/internal/updatecoord/state"
)

// Optional only: unenrolled/development/package-managed executables do no updater
// database work. This is not installation enrollment or package ownership proof.
func startInstalledTUINotices(srv *server.Server) func() {
	none := func() {}
	exe, e := os.Executable()
	if e != nil {
		return none
	}
	// Missing adjacent identity is the normal case and needs no warning or loop.
	if _, e = os.Lstat(filepath.Join(filepath.Dir(exe), enrollment.FileName)); os.IsNotExist(e) {
		return none
	} else if e != nil {
		return none
	}
	home, e := os.UserHomeDir()
	if e != nil {
		return none
	}
	root, e := state.StateRoot(runtime.GOOS, home, os.Getenv("LOCALAPPDATA"), os.Getenv("XDG_STATE_HOME"))
	if e != nil {
		return none
	}
	ctx, cancel := context.WithCancel(context.Background())
	go srv.WatchEnrolledTUIUpdates(ctx, exe, root, func(error) {
		// Keep paths/database content out of diagnostics. A failure is not an agent
		// readiness failure and must not block conversations or reconnection.
		log.Print("[updates] installed TUI notification unavailable; agent operation is unaffected")
	})
	return cancel
}
