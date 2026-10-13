package server

import (
	"context"
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
