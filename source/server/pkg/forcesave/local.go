// Package forcesave exposes only local, protected save-failure decisions to clients.
// It neither requests an initial force stop nor grants network control authority.
package forcesave

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"cercano/source/server/internal/updatecoord/enrollment"
	"cercano/source/server/internal/updatecoord/state"
)

var ErrUnavailable = errors.New("local update decision unavailable or expired")

type Receipt struct {
	ID          string
	OperationID int64
	Expires     time.Time
}
type Local struct {
	launcher, root string
	identity       enrollment.Record
}

func Discover(ctx context.Context) (*Local, error) {
	exe, e := os.Executable()
	if e != nil {
		return nil, e
	}
	if _, e = os.Lstat(filepath.Join(filepath.Dir(exe), enrollment.FileName)); os.IsNotExist(e) {
		return nil, nil
	} else if e != nil {
		return nil, e
	}
	identity, e := enrollment.DiscoverStableLauncher(ctx, exe)
	if e != nil {
		return nil, e
	}
	home, e := os.UserHomeDir()
	if e != nil {
		return nil, e
	}
	root, e := state.StateRoot(runtime.GOOS, home, os.Getenv("LOCALAPPDATA"), os.Getenv("XDG_STATE_HOME"))
	if e != nil {
		return nil, e
	}
	return &Local{exe, root, identity}, nil
}
func (l *Local) open(ctx context.Context) (*state.ForceControl, error) {
	if l == nil {
		return nil, ErrUnavailable
	}
	identity, e := enrollment.DiscoverStableLauncher(ctx, l.launcher)
	if e != nil || identity != l.identity {
		return nil, ErrUnavailable
	}
	return state.OpenExistingForceControl(ctx, l.root, l.identity.InstallID)
}
func (l *Local) Pending(ctx context.Context) (Receipt, bool, error) {
	c, e := l.open(ctx)
	if e != nil {
		return Receipt{}, false, e
	}
	if c == nil {
		return Receipt{}, false, nil
	}
	defer c.Close()
	f, ok, e := c.PendingSaveDecision(ctx, time.Now)
	return Receipt{f.ReceiptID, f.OpID, time.UnixMilli(f.ExpiresMS)}, ok, e
}

// Decide requires a fresh confirmation argument for Force anyway. Cancel does
// not stop work. Submission is not evidence of stop, exit, or update completion.
func (l *Local) Decide(ctx context.Context, r Receipt, force, confirmed bool) error {
	if force && !confirmed || r.ID == "" || !time.Now().Before(r.Expires) {
		return ErrUnavailable
	}
	c, e := l.open(ctx)
	if e != nil {
		return e
	}
	if c == nil {
		return ErrUnavailable
	}
	defer c.Close()
	f, ok, e := c.PendingSaveDecision(ctx, time.Now)
	if e != nil {
		return e
	}
	if !ok || f.ReceiptID != r.ID || f.OpID != r.OperationID || f.ExpiresMS != r.Expires.UnixMilli() {
		return ErrUnavailable
	}
	decision := state.ForceDecisionCancel
	if force {
		decision = state.ForceDecisionForce
	}
	return c.SubmitSaveDecision(ctx, r.ID, decision, confirmed, time.Now)
}
