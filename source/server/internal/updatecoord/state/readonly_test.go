package state

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"cercano/source/server/internal/updatecoord/privdir"
)

func readonlyFixture(t *testing.T) (string, *Store) {
	t.Helper()
	root := t.TempDir()
	dir := root
	for _, part := range []string{orgComponent(), "updater", "test-install"} {
		dir = filepath.Join(dir, part)
		if _, e := privdir.Ensure(dir); e != nil {
			t.Fatal(e)
		}
	}
	s, e := Open(root, "test-install")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return root, s
}
func TestReadOnlyDoesNotInitializeOrMigrate(t *testing.T) {
	root := t.TempDir()
	if s, e := OpenExistingReadOnly(context.Background(), root, "test-install"); e == nil {
		s.Close()
		t.Fatal("missing state initialized")
	}
	entries, e := os.ReadDir(root)
	if e != nil || len(entries) != 0 {
		t.Fatal("missing state created paths", entries, e)
	}
	root, s := readonlyFixture(t)
	path := filepath.Join(s.Directory(), stateFileName)
	s.Close()
	data, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	for _, version := range []int64{LegacySchemaVersion, SchemaVersion + 1} {
		changed := append([]byte(nil), data...)
		binary.BigEndian.PutUint32(changed[60:64], uint32(version))
		if e = os.WriteFile(path, changed, 0600); e != nil {
			t.Fatal(e)
		}
		opened, e := OpenExistingReadOnly(context.Background(), root, "test-install")
		if e == nil {
			opened.Close()
			t.Fatal("unsupported version opened", version)
		}
		want := ErrReadNeedsUpgrade
		if version > SchemaVersion {
			want = ErrFutureSchema
		}
		if !errors.Is(e, want) {
			t.Fatal(e)
		}
		after, e := os.ReadFile(path)
		if e != nil || sha256.Sum256(after) != sha256.Sum256(changed) {
			t.Fatal("reader changed schema/database", e)
		}
	}
}
func TestReadOnlySeesCommittedWALAndRefusesWrites(t *testing.T) {
	root, s := readonlyFixture(t)
	s.Close() // Ensure recognized main-file header.
	writer, e := Open(root, "test-install")
	if e != nil {
		t.Fatal(e)
	}
	defer writer.Close()
	op := startCurrentOperation(t, writer, "2.0.0")
	reader, e := OpenExistingReadOnly(context.Background(), root, "test-install")
	if e != nil {
		t.Fatal(e)
	}
	defer reader.Close()
	record, revision, e := reader.LoadOperationRecord(context.Background(), op.ID)
	if e != nil || record.TargetVersion != "2.0.0" {
		t.Fatal("missed committed WAL", record, e)
	}
	if _, e = reader.SaveOperationRecord(context.Background(), revision, record); e == nil {
		t.Fatal("read-only store wrote record")
	}
	if _, e = reader.AllocateOperationID(context.Background()); e == nil {
		t.Fatal("read-only store allocated operation")
	}
	if _, e = reader.db.Exec("PRAGMA query_only=OFF; UPDATE state_meta SET value='bad' WHERE key='install_id'"); e == nil {
		t.Fatal("mode=ro bypassed")
	}
	got, r, e := writer.LoadOperationRecord(context.Background(), op.ID)
	if e != nil || got.ID != record.ID || r != revision {
		t.Fatal("refused writes changed record", r, e)
	}
}
func TestReadOnlyRefusesForeignFileWithoutTouchingIt(t *testing.T) {
	root, s := readonlyFixture(t)
	path := filepath.Join(s.Directory(), stateFileName)
	s.Close()
	raw := make([]byte, 100)
	copy(raw, "not a database")
	if e := os.WriteFile(path, raw, 0600); e != nil {
		t.Fatal(e)
	}
	if reader, e := OpenExistingReadOnly(context.Background(), root, "test-install"); !errors.Is(e, ErrForeignDatabase) {
		if reader != nil {
			reader.Close()
		}
		t.Fatal(e)
	}
	after, e := os.ReadFile(path)
	if e != nil || sha256.Sum256(after) != sha256.Sum256(raw) {
		t.Fatal("foreign file changed", e)
	}
}
