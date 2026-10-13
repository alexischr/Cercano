package server

import (
	"context"
	"os"
	"time"

	"cercano/source/server/internal/updatecoord/enrollment"
)

// WatchEnrolledTUIUpdates runs inside the existing agent lifetime, never as a
// service/process. It exits immediately for an unenrolled launcher. Once bound,
// it observes completed installs without requiring a helper-to-agent command RPC.
// Diagnostics are deduplicated; no failure here should stop the agent.
func (s *Server) WatchEnrolledTUIUpdates(ctx context.Context, launcher, stateRoot string, diagnostic func(error)) {
	s.watchEnrolledTUIUpdates(ctx, launcher, stateRoot, 5*time.Second, diagnostic)
}
func (s *Server) watchEnrolledTUIUpdates(ctx context.Context, launcher, stateRoot string, interval time.Duration, diagnostic func(error)) {
	if ctx == nil || interval <= 0 {
		return
	}
	enrolled, e := enrollment.DiscoverStableLauncher(ctx, launcher)
	if e != nil {
		if !os.IsNotExist(e) && diagnostic != nil {
			diagnostic(e)
		}
		return
	}
	forceOwner, forceErr := s.newForceLocalOwner()
	if forceErr != nil && diagnostic != nil {
		diagnostic(forceErr)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	lastError := ""
	lastControlError := ""
	for {
		probe, cancel := context.WithTimeout(ctx, 2*time.Second)
		current, e := enrollment.DiscoverStableLauncher(probe, launcher)
		if e == nil && current != enrolled {
			cancel()
			return
		} // never silently rebind identity
		if e == nil && forceOwner != nil {
			stopped, controlErr := forceOwner.poll(probe, s, stateRoot, enrolled.InstallID)
			if stopped {
				cancel()
				return
			}
			if controlErr != nil {
				if diagnostic != nil && controlErr.Error() != lastControlError {
					diagnostic(controlErr)
				}
				lastControlError = controlErr.Error()
			} else {
				lastControlError = ""
			}
		}
		if e == nil {
			_, e = s.SyncEnrolledTUIUpdate(probe, launcher, stateRoot)
		}
		cancel()
		if ctx.Err() != nil {
			return
		}
		if e != nil {
			if diagnostic != nil && e.Error() != lastError {
				diagnostic(e)
			}
			lastError = e.Error()
		} else {
			lastError = ""
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
