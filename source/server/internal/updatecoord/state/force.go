package state

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"math"
	"time"

	"cercano/source/server/internal/updatecoord/operation"
)

var (
	ErrForceIntent     = errors.New("state: invalid or unconfirmed force intent")
	ErrForceTarget     = errors.New("state: stale force target")
	ErrForceReplay     = errors.New("state: force intent already exists or consumed")
	ErrForceUnprepared = errors.New("state: force operation is not confirmed and prepared")
)

const MaxForceIntentTTL = 30 * time.Second

// ForceAgent identifies one process lifetime. BootID is random public identity,
// not a bearer credential; authorization is the protected local store. A PID alone
// never authorizes anything. The runtime keeps its BootID in memory across polls.
type ForceAgent struct {
	BootID string
	PID    int64
}

func NewForceAgent(pid int64) (ForceAgent, error) {
	var b [32]byte
	if _, e := rand.Read(b[:]); e != nil {
		return ForceAgent{}, e
	}
	a := ForceAgent{hex.EncodeToString(b[:]), pid}
	return a, a.validate()
}
func (a ForceAgent) validate() error {
	if !IsLowerHexSHA256(a.BootID) || a.PID <= 0 || a.PID > math.MaxInt32 {
		return ErrForceTarget
	}
	return nil
}

type ForceIntent struct {
	OpID                int64
	Agent               ForceAgent
	IssuedMS, ExpiresMS int64
}

func (f ForceIntent) validate(now time.Time) error {
	n := now.UnixMilli()
	if f.OpID <= 0 || f.Agent.validate() != nil || n <= 0 || f.IssuedMS <= 0 || f.IssuedMS > n || f.ExpiresMS <= n || f.ExpiresMS <= f.IssuedMS || f.ExpiresMS-f.IssuedMS > MaxForceIntentTTL.Milliseconds() {
		return ErrForceIntent
	}
	return nil
}
func (s *Store) forceAgentConn(ctx context.Context, c dbtx) (ForceAgent, bool, error) {
	var a ForceAgent
	var id, bt, pt, it string
	e := c.QueryRowContext(ctx, `SELECT install_id,boot_id,pid,typeof(install_id),typeof(boot_id),typeof(pid) FROM force_agent_sessions`).Scan(&id, &a.BootID, &a.PID, &it, &bt, &pt)
	if errors.Is(e, sql.ErrNoRows) {
		return a, false, nil
	}
	if e != nil {
		return a, false, e
	}
	var count int
	if e = c.QueryRowContext(ctx, `SELECT COUNT(*) FROM force_agent_sessions`).Scan(&count); e != nil {
		return a, false, e
	}
	if count != 1 || id != s.installID || it != "text" || bt != "text" || pt != "integer" || a.validate() != nil {
		return a, false, ErrCorruptDatabase
	}
	return a, true, nil
}

// RegisterForceAgent is called once by an already-owned agent lifetime, never on
// every poll. A replacement session revokes old-target requests. It does not prove
// the previous process exited, authorize killing it, or create/migrate a store.
func (s *Store) RegisterForceAgent(ctx context.Context, a ForceAgent) error {
	if e := a.validate(); e != nil {
		return e
	}
	return s.inWriteTx(ctx, func(ctx context.Context, c *sql.Conn) error {
		previous, found, e := s.forceAgentConn(ctx, c)
		if e != nil {
			return e
		}
		if found && previous == a {
			return nil
		}
		_, e = c.ExecContext(ctx, `INSERT INTO force_agent_sessions(install_id,boot_id,pid) VALUES(?,?,?) ON CONFLICT(install_id) DO UPDATE SET boot_id=excluded.boot_id,pid=excluded.pid`, s.installID, a.BootID, a.PID)
		return e
	})
}
func (s *Store) ReadForceAgent(ctx context.Context) (ForceAgent, bool, error) {
	return s.forceAgentConn(ctx, s.db)
}
func (s *Store) forceEligible(ctx context.Context, c *sql.Conn, opID int64) error {
	rec, _, found, e := s.loadCurrentOperationConn(ctx, c)
	if e != nil {
		return e
	}
	if !found || rec.ID != opID || !rec.ConsentRecorded || rec.State != operation.StateDraining || rec.RecoveryNeeded || rec.RecoveryRequested {
		return ErrForceUnprepared
	}
	var payload, id string
	var revision int64
	e = c.QueryRowContext(ctx, `SELECT install_id,revision,journal_json FROM activation_journals WHERE op_id=?`, opID).Scan(&id, &revision, &payload)
	if errors.Is(e, sql.ErrNoRows) {
		return ErrForceUnprepared
	}
	if e != nil {
		return e
	}
	j, e := decodeActivationJournal([]byte(payload))
	if e != nil {
		return e
	}
	if id != s.installID || j.InstallID != s.installID || j.OpID != opID || revision < 1 {
		return ErrCorruptDatabase
	}
	if j.TargetVersion != rec.TargetVersion || j.Checkpoint != JournalPrepared {
		return ErrForceUnprepared
	}
	return nil
}
func (s *Store) forceIntentConn(ctx context.Context, c dbtx, opID int64) (ForceIntent, bool, bool, error) {
	f := ForceIntent{OpID: opID}
	var id, it, bt, pt, issuedt, expirest, ct, confirmedt string
	var consumed, confirmed int
	e := c.QueryRowContext(ctx, `SELECT install_id,boot_id,pid,issued_ms,expires_ms,consumed,typeof(install_id),typeof(boot_id),typeof(pid),typeof(issued_ms),typeof(expires_ms),typeof(consumed),confirmed,typeof(confirmed) FROM force_stop_intents WHERE op_id=?`, opID).Scan(&id, &f.Agent.BootID, &f.Agent.PID, &f.IssuedMS, &f.ExpiresMS, &consumed, &it, &bt, &pt, &issuedt, &expirest, &ct, &confirmed, &confirmedt)
	if errors.Is(e, sql.ErrNoRows) {
		return f, false, false, nil
	}
	if e != nil {
		return f, false, false, e
	}
	if confirmed != 1 || confirmedt != "integer" || id != s.installID || it != "text" || bt != "text" || pt != "integer" || issuedt != "integer" || expirest != "integer" || ct != "integer" || f.Agent.validate() != nil || (consumed != 0 && consumed != 1) || f.IssuedMS <= 0 || f.ExpiresMS <= f.IssuedMS || f.ExpiresMS-f.IssuedMS > MaxForceIntentTTL.Milliseconds() {
		return f, false, false, ErrCorruptDatabase
	}
	return f, true, consumed == 1, nil
}

// SubmitForceIntent requires force-specific consent as well as the operation's
// existing consent and prepared artifact journal. Requests are never overwritten.
func (s *Store) SubmitForceIntent(ctx context.Context, f ForceIntent, confirmed bool, now func() time.Time) error {
	if !confirmed || now == nil {
		return ErrForceIntent
	}
	if e := f.validate(now()); e != nil {
		return e
	}
	return s.inWriteTx(ctx, func(ctx context.Context, c *sql.Conn) error {
		if e := f.validate(now()); e != nil {
			return e
		}
		if e := s.forceEligible(ctx, c, f.OpID); e != nil {
			return e
		}
		a, found, e := s.forceAgentConn(ctx, c)
		if e != nil {
			return e
		}
		if !found || a != f.Agent {
			return ErrForceTarget
		}
		_, found, _, e = s.forceIntentConn(ctx, c, f.OpID)
		if e != nil {
			return e
		}
		if found {
			return ErrForceReplay
		}
		_, e = c.ExecContext(ctx, `INSERT INTO force_stop_intents(op_id,install_id,boot_id,pid,issued_ms,expires_ms,consumed,confirmed) VALUES(?,?,?,?,?,?,0,1)`, f.OpID, s.installID, f.Agent.BootID, f.Agent.PID, f.IssuedMS, f.ExpiresMS)
		if e != nil {
			return e
		}
		return f.validate(now())
	})
}

// ConsumeForceIntent uses the caller's memory-held lifetime identity and current
// operation. Commit precedes interruption. A crash after consumption does not
// replay the request: the updater must reconcile actual process state. A consumed
// intent is NOT process-exit, installation or health proof.
func (s *Store) ConsumeForceIntent(ctx context.Context, owned ForceAgent, now func() time.Time) (ForceIntent, bool, error) {
	var result ForceIntent
	claimed := false
	if now == nil {
		return result, false, ErrForceIntent
	}
	if e := owned.validate(); e != nil {
		return result, false, e
	}
	e := s.inWriteTx(ctx, func(ctx context.Context, c *sql.Conn) error {
		a, found, e := s.forceAgentConn(ctx, c)
		if e != nil {
			return e
		}
		if !found || a != owned {
			return ErrForceTarget
		}
		rec, _, found, e := s.loadCurrentOperationConn(ctx, c)
		if e != nil || !found {
			return e
		}
		f, found, consumed, e := s.forceIntentConn(ctx, c, rec.ID)
		if e != nil || !found {
			return e
		}
		if consumed {
			return ErrForceReplay
		}
		if f.Agent != owned {
			return ErrForceTarget
		}
		if e = f.validate(now()); e != nil {
			return e
		}
		if e = s.forceEligible(ctx, c, rec.ID); e != nil {
			return e
		}
		r, e := c.ExecContext(ctx, `UPDATE force_stop_intents SET consumed=1 WHERE op_id=? AND install_id=? AND consumed=0`, f.OpID, s.installID)
		if e != nil {
			return e
		}
		n, e := r.RowsAffected()
		if e != nil {
			return e
		}
		if n != 1 {
			return ErrForceReplay
		}
		if e := f.validate(now()); e != nil {
			return e
		}
		result = f
		claimed = true
		return nil
	})
	if e != nil {
		return ForceIntent{}, false, e
	}
	return result, claimed, nil
}

// ReadForceIntent is diagnostic only; a consumed flag is never process-exit proof.
func (s *Store) ReadForceIntent(ctx context.Context, opID int64) (ForceIntent, bool, bool, error) {
	if opID <= 0 {
		return ForceIntent{}, false, false, ErrForceIntent
	}
	return s.forceIntentConn(ctx, s.db, opID)
}
