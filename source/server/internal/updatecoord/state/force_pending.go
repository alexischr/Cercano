package state

import (
	"context"
	"database/sql"
	"time"
)

// PendingSaveDecision returns at most one actionable, undecided receipt for the
// current prepared operation and registered lifetime. It creates no authority.
func (c *ForceControl) PendingSaveDecision(ctx context.Context, now func() time.Time) (ForceSaveFailure, bool, error) {
	if c == nil || c.store == nil {
		return ForceSaveFailure{}, false, ErrSaveFailure
	}
	s := c.store
	if now == nil {
		return ForceSaveFailure{}, false, ErrSaveFailure
	}
	var result ForceSaveFailure
	found := false
	e := s.inWriteTx(ctx, func(ctx context.Context, conn *sql.Conn) error {
		rows, e := conn.QueryContext(ctx, `SELECT receipt_id FROM force_save_failures WHERE consumed=0 AND decision=0 AND expires_ms>?`, now().UnixMilli())
		if e != nil {
			return e
		}
		var ids []string
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return e
			}
			ids = append(ids, id)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		for _, id := range ids {
			f, ok, e := s.saveFailureConn(ctx, conn, id)
			if e != nil {
				return e
			}
			if !ok {
				return ErrCorruptDatabase
			}
			if e = f.validate(now()); e != nil {
				return e
			}
			if e = s.saveFailureEligible(ctx, conn, f); e != nil {
				return e
			}
			if found {
				return ErrSaveFailure
			}
			result = f
			found = true
		}
		return nil
	})
	if e != nil {
		return ForceSaveFailure{}, false, e
	}
	return result, found, nil
}
