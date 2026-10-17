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
	"cercano/source/server/pkg/installcontext"
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
// DiscoverVerified returns the local decision surface for a caller whose
// installation context was already verified by installcontext's verification
// constructor (VerifyChildVerified). It never inspects os.Executable() or
// argv: the stable launcher comes from the verified record's proven role,
// and the enrollment record is re-read from disk beside that launcher and
// must match the verified record EXACTLY — no ancestor search, no re-
// enrollment, no locator trust. A zero/absent Verified is refused. The
// per-user state root keeps its existing environment derivation and
// nothing is provisioned or migrated; missing state fails closed on use
// (Pending/Decide), as with Discover.
func DiscoverVerified(ctx context.Context, verified installcontext.Verified) (*Local, error) {
	launcher, e := verified.StableLauncher()
	if e != nil {
		return nil, ErrUnavailable
	}
	identity, e := enrollment.DiscoverStableLauncher(ctx, launcher)
	if e != nil {
		return nil, e
	}
	if identity != verified.Record() {
		return nil, ErrUnavailable
	}
	home, e := os.UserHomeDir()
	if e != nil {
		return nil, e
	}
	root, e := state.StateRoot(runtime.GOOS, home, os.Getenv("LOCALAPPDATA"), os.Getenv("XDG_STATE_HOME"))
	if e != nil {
		return nil, e
	}
	return &Local{launcher, root, identity}, nil
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
