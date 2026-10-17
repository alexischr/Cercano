package server

import (
	"context"
	"errors"
	"os"

	"cercano/source/server/internal/updatecoord/enrollment"
	"cercano/source/server/internal/updatecoord/state"
)

// SyncEnrolledTUIUpdate discovers only an explicit enrolled stable launcher and
// reads only already-current updater state. Missing enrollment/state is normal
// for development, package-managed and uninitialized installations. Errors must
// remain diagnostic, not startup or cross-version connection gates.
func (s *Server) SyncEnrolledTUIUpdate(ctx context.Context, stableLauncher, stateRoot string) (bool, error) {
	record, e := enrollment.DiscoverStableLauncher(ctx, stableLauncher)
	if os.IsNotExist(e) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	store, e := state.OpenExistingReadOnly(ctx, stateRoot, record.InstallID)
	if os.IsNotExist(e) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	defer store.Close()
	return s.SyncTUIUpdateFromState(ctx, store)
}

// SyncBoundEnrolledTUIUpdate discovers the enrolled stable launcher and verifies
// it matches the expected record before opening state. This prevents cross-install
// identity confusion. Missing enrollment/state is normal for development,
// package-managed and uninitialized installations. Errors must remain diagnostic,
// not startup or cross-version connection gates.
func (s *Server) SyncBoundEnrolledTUIUpdate(ctx context.Context, stableLauncher, stateRoot string, expected *enrollment.Record) (bool, error) {
	if ctx == nil || expected == nil {
		return false, errors.New("missing enrollment binding")
	}
	bound := *expected
	record, e := enrollment.DiscoverStableLauncher(ctx, stableLauncher)
	if os.IsNotExist(e) {
		return false, nil
	}
	if e != nil {
		return false, e
	}

	// Verify the discovered enrollment matches the expected record
	if record != bound {
		return false, errors.New("enrollment record mismatch: discovered record does not match expected")
	}

	store, e := state.OpenExistingReadOnly(ctx, stateRoot, record.InstallID)
	if os.IsNotExist(e) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	defer store.Close()
	return s.syncTUIUpdateFromState(ctx, store, func() error {
		current, err := enrollment.DiscoverStableLauncher(ctx, stableLauncher)
		if err != nil {
			return err
		}
		if current != bound {
			return errors.New("enrollment changed during observation")
		}
		return nil
	})
}
