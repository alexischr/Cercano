package state

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"cercano/source/server/internal/updatecoord/privdir"
)

var ErrReadNeedsUpgrade = errors.New("state: existing database needs installer initialization or migration; optional read unavailable")

// OpenExistingReadOnly never initializes a database, creates directories, changes
// permissions, migrates schemas or allows SQL writes. SQLite may use/create its
// normal WAL/SHM coordination files beside a recognized database; mode=ro does
// not mean zero filesystem activity. Do not use immutable=1: it can miss WAL data.
// Optional notification failure must never prevent ordinary agent startup.
func OpenExistingReadOnly(parent context.Context, stateRoot, installID string) (*Store, error) {
	if parent == nil {
		return nil, ErrInvalidStateRoot
	}
	ctx, cancel := context.WithTimeout(parent, opTimeout)
	defer cancel()
	if e := ValidateInstallID(installID); e != nil {
		return nil, e
	}
	if !filepath.IsAbs(stateRoot) || filepath.Clean(stateRoot) != stateRoot {
		return nil, ErrInvalidStateRoot
	}
	root, e := os.Lstat(stateRoot)
	if e != nil {
		return nil, e
	}
	if !root.IsDir() || root.Mode()&os.ModeSymlink != 0 {
		return nil, ErrUnsafePath
	}
	dir := stateRoot
	for _, part := range []string{orgComponent(), "updater", installID} {
		dir = filepath.Join(dir, part)
		if e = privdir.VerifyExisting(dir); e != nil {
			return nil, e
		}
	}
	path := filepath.Join(dir, stateFileName)
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		info, err := os.Lstat(path + suffix)
		if suffix != "" && os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || (runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0) {
			return nil, ErrUnsafePath
		}
	}
	// Refuse obviously foreign/older main files before asking SQLite to coordinate.
	// A newly initialized/migrated schema present only in WAL is conservatively
	// unavailable until the installer checkpoints it; startup never repairs it.
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	var header [100]byte
	_, e = io.ReadFull(f, header[:])
	f.Close()
	if e != nil {
		return nil, ErrCorruptDatabase
	}
	if string(header[:16]) != "SQLite format 3\x00" || int64(binary.BigEndian.Uint32(header[68:72])) != AppID {
		return nil, ErrForeignDatabase
	}
	version := int64(binary.BigEndian.Uint32(header[60:64]))
	if version > SchemaVersion {
		return nil, ErrFutureSchema
	}
	if version != SchemaVersion {
		return nil, ErrReadNeedsUpgrade
	}
	db, e := sql.Open("sqlite", sqliteURI(path, "ro"))
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	fail := func(e error) (*Store, error) { db.Close(); return nil, e }
	if _, e = db.ExecContext(ctx, "PRAGMA query_only=ON"); e != nil {
		return fail(e)
	}
	conn, e := db.Conn(ctx)
	if e != nil {
		return fail(e)
	}
	if _, e = conn.ExecContext(ctx, "BEGIN"); e != nil {
		conn.Close()
		return fail(e)
	}
	e = validateExistingDB(ctx, conn, installID)
	if e == nil {
		var v int64
		v, e = readSchemaVersion(ctx, conn)
		if e == nil && v != SchemaVersion {
			e = ErrReadNeedsUpgrade
		}
	}
	rollbackCtx, rollbackCancel := context.WithTimeout(context.Background(), opTimeout)
	_, rollbackErr := conn.ExecContext(rollbackCtx, "ROLLBACK")
	rollbackCancel()
	conn.Close()
	if e != nil {
		return fail(e)
	}
	if rollbackErr != nil {
		return fail(rollbackErr)
	}
	return &Store{db: db, installID: installID, dir: dir}, nil
}
