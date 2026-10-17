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
	"cercano/source/server/pkg/installcontext"
)

// startInstalledTUINotices runs the installed-TUI update observer for the
// agent lifetime. The observer is a pure reader: no updater-state mutation,
// no protocol negotiation, no enrollment.
//
// With a verified startup context the observer uses the verified stable
// launcher and requires the enrollment beside it to match the startup
// verified record EXACTLY before binding — a stale startup record (the
// installation changed on disk after this agent verified) must not
// silently rebind the watch to the new on-disk identity. Once bound, the
// existing watch revalidation (WatchEnrolledTUIUpdates never rebinds
// mid-loop) governs.
//
// Without a verified context (the agent was not launched as a verified
// installed member — no locators) behavior is unchanged: optional only,
// unenrolled/development/package-managed executables do no updater
// database work. This is not installation enrollment or package ownership
// proof.
func startInstalledTUINotices(verified installcontext.Verified, srv *server.Server) func() {
	none := func() {}
	ctx := context.Background()
	var launcher string
	if verified.Present() {
		// Never observe this process's own executable: its path is
		// attacker-controlled at exec time. The verified record is the
		// only source of the stable launcher and identity.
		stable, e := verified.StableLauncher()
		if e != nil {
			return none
		}
		// Exact initial enrollment match: the record beside the verified
		// stable launcher must still equal the startup verified record.
		// Any mismatch refuses the observer outright.
		current, e := enrollment.DiscoverStableLauncher(ctx, stable)
		if e != nil || current != verified.Record() {
			return none
		}
		launcher = stable
	} else {
		// No verified context: unchanged pre-verification behavior. A
		// missing adjacent identity is the normal case and needs no
		// warning or loop.
		exe, e := os.Executable()
		if e != nil {
			return none
		}
		if _, e = os.Lstat(filepath.Join(filepath.Dir(exe), enrollment.FileName)); os.IsNotExist(e) {
			return none
		} else if e != nil {
			return none
		}
		launcher = exe
	}
	home, e := os.UserHomeDir()
	if e != nil {
		return none
	}
	root, e := state.StateRoot(runtime.GOOS, home, os.Getenv("LOCALAPPDATA"), os.Getenv("XDG_STATE_HOME"))
	if e != nil {
		return none
	}
	watchCtx, cancel := context.WithCancel(ctx)
	go srv.WatchEnrolledTUIUpdates(watchCtx, launcher, root, func(error) {
		// Keep paths/database content out of diagnostics. A failure is not an agent
		// readiness failure and must not block conversations or reconnection.
		log.Print("[updates] installed TUI notification unavailable; agent operation is unaffected")
	})
	return cancel
}
