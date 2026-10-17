package telemetry

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"modernc.org/sqlite"
)

// Intercept real SQLite statements to cancel at a deterministic point inside
// initialization, without adding production hooks or depending on timing.
type cancelAccountingConnector struct {
	dsn          string
	cancel       context.CancelFunc
	once         sync.Once
	failRollback bool
}

func (c *cancelAccountingConnector) Driver() driver.Driver { return &sqlite.Driver{} }
func (c *cancelAccountingConnector) Connect(context.Context) (driver.Conn, error) {
	conn, err := c.Driver().Open(c.dsn)
	if err != nil {
		return nil, err
	}
	return &cancelAccountingConn{Conn: conn, connector: c}, nil
}

type cancelAccountingConn struct {
	driver.Conn
	connector *cancelAccountingConnector
}

func (c *cancelAccountingConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if query == "ROLLBACK" && c.connector.failRollback {
		return nil, errors.New("injected rollback failure")
	}
	result, err := c.Conn.(driver.ExecerContext).ExecContext(ctx, query, args)
	if err == nil && strings.HasPrefix(query, "CREATE TABLE IF NOT EXISTS accounting_metadata") {
		c.connector.once.Do(c.connector.cancel)
	}
	return result, err
}

func TestAccountingInitializationCancellationReleasesTransaction(t *testing.T) {
	for _, failRollback := range []bool{false, true} {
		name := "rollback"
		if failRollback {
			name = "discard-on-rollback-failure"
		}
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "accounting.db")
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			connector := &cancelAccountingConnector{dsn: path, cancel: cancel, failRollback: failRollback}
			db := sql.OpenDB(connector)
			defer db.Close()
			db.SetMaxOpenConns(1)
			store := &SQLiteStore{db: db}
			if err := store.InitializeAccounting(ctx); !errors.Is(err, context.Canceled) {
				t.Fatalf("expected cancellation inside initialization, got %v", err)
			}
			// A separate connection detects a leaked writer lock without waiting for
			// busy_timeout. It also verifies the partial schema was rolled back.
			other, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			defer other.Close()
			conn, err := other.Conn(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			if _, err := conn.ExecContext(t.Context(), "BEGIN IMMEDIATE"); err != nil {
				t.Fatalf("canceled initialization leaked writer lock: %v", err)
			}
			defer conn.ExecContext(context.Background(), "ROLLBACK")
			var tables int
			if err := conn.QueryRowContext(t.Context(), "SELECT count(*) FROM sqlite_master WHERE name='accounting_metadata'").Scan(&tables); err != nil {
				t.Fatal(err)
			}
			if tables != 0 {
				t.Fatal("canceled initialization left partial schema")
			}
			if _, err := conn.ExecContext(t.Context(), "ROLLBACK"); err != nil {
				t.Fatal(err)
			}
			if err := store.InitializeAccounting(t.Context()); err != nil {
				t.Fatalf("pooled connection unusable after cancellation: %v", err)
			}
		})
	}
}
