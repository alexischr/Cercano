package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"cercano/source/server/internal/updatecoord/imagecheck"
	"cercano/source/server/internal/updatecoord/operation"
)

// validManifest returns a structurally valid manifest bound to the given
// current operation, mirroring what archivecheck verified for an agent +
// client release.
func validManifest(opID int64, target string) ActivationManifest {
	return ActivationManifest{
		SchemaVersion:          ActivationManifestSchemaVersion,
		InstallID:              "test-install",
		OpID:                   opID,
		TargetVersion:          target,
		StagedVersionDir:       "staged-9.9.9",
		VerifiedArtifactSHA256: strings.Repeat("a", 64),
		Members: []ManifestMember{
			{Path: "cercano-agent.exe", Length: 111, SHA256: strings.Repeat("f", 64)},
			{Path: "bin/cercano-client.exe", Length: 222, SHA256: strings.Repeat("0", 64)},
		},
	}
}

func TestActivationManifestRoundTripAndPersistence(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s, err := Open(root, "test-install")
	if err != nil {
		t.Fatal(err)
	}
	snap := startCurrentOperation(t, s, "9.9.9")
	m := validManifest(snap.ID, "9.9.9")

	rev, err := s.SaveActivationManifest(ctx, m)
	if err != nil || rev != 1 {
		t.Fatalf("save %d %v", rev, err)
	}
	loaded, rev, err := s.LoadActivationManifest(ctx, snap.ID)
	if err != nil || rev != 1 || !reflect.DeepEqual(loaded, m) {
		t.Fatalf("load mismatch: %+v %d %v", loaded, rev, err)
	}
	// The independent current-operation reader returns the same fact.
	current, crev, err := s.LoadCurrentActivationManifest(ctx)
	if err != nil || crev != 1 || !reflect.DeepEqual(current, m) {
		t.Fatalf("current reader mismatch: %+v %d %v", current, crev, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	// The manifest survives a store reopen byte-identically.
	s2, err := Open(root, "test-install")
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	reloaded, rev, err := s2.LoadActivationManifest(ctx, snap.ID)
	if err != nil || rev != 1 || !reflect.DeepEqual(reloaded, m) {
		t.Fatalf("reload after reopen: %+v %d %v", reloaded, rev, err)
	}

	// A manifest is written exactly once: any second save — identical or
	// mutated — is refused with ErrStaleRevision and nothing changes.
	_, err = s2.SaveActivationManifest(ctx, m)
	if !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("identical re-save: %v", err)
	}
	mutated := validManifest(snap.ID, "9.9.9")
	mutated.Members[0].SHA256 = strings.Repeat("e", 64)
	_, err = s2.SaveActivationManifest(ctx, mutated)
	if !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("mutated re-save: %v", err)
	}
	if got, rev, err := s2.LoadActivationManifest(ctx, snap.ID); err != nil || rev != 1 || !reflect.DeepEqual(got, m) {
		t.Fatalf("manifest changed after refused re-saves: %+v %d %v", got, rev, err)
	}

	// Unknown operations have no manifest.
	if _, _, err := s2.LoadActivationManifest(ctx, snap.ID+9999); !errors.Is(err, ErrRecordNotFound) {
		t.Fatalf("unknown manifest load: %v", err)
	}
}

func TestActivationManifestRequiresExistingCurrentOperation(t *testing.T) {
	ctx := context.Background()

	// With no operation at all there is nothing to own a manifest.
	s, err := Open(t.TempDir(), "test-install")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.SaveActivationManifest(ctx, validManifest(1, "9.9.9")); !errors.Is(err, ErrRecordNotFound) {
		t.Fatalf("manifest without operation: %v", err)
	}

	// A manifest may only be attached to the installation's CURRENT
	// operation: an earlier, superseded operation is refused.
	root := t.TempDir()
	s2, err := Open(root, "test-install")
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	first := startCurrentOperation(t, s2, "9.9.9")
	// A superseded operation: cancel the first operation, then start a
	// replacement (the same pattern activation_test.go uses).
	if _, err := NewAdapter(s2).Apply(ctx, operation.Input{Event: operation.EventCancel, OperationID: first.ID}); err != nil {
		t.Fatal(err)
	}
	startCurrentOperation(t, s2, "9.9.10")

	stale := validManifest(first.ID, "9.9.9")
	if _, err := s2.SaveActivationManifest(ctx, stale); !errors.Is(err, ErrNotCurrentOperation) {
		t.Fatalf("superseded operation manifest: %v", err)
	}
	if _, _, err := s2.LoadActivationManifest(ctx, first.ID); !errors.Is(err, ErrRecordNotFound) {
		t.Fatalf("superseded manifest must not exist: %v", err)
	}
	// The refused save wrote nothing for the current operation either.
	if _, _, err := s2.LoadCurrentActivationManifest(ctx); !errors.Is(err, ErrRecordNotFound) {
		t.Fatalf("refused save leaked a row: %v", err)
	}

	// The current operation accepts its manifest; the stale one stays
	// refused even afterwards.
	current := validManifest(first.ID+1, "9.9.10")
	if rev, err := s2.SaveActivationManifest(ctx, current); err != nil || rev != 1 {
		t.Fatalf("current manifest save %d %v", rev, err)
	}
	if _, err := s2.SaveActivationManifest(ctx, stale); !errors.Is(err, ErrNotCurrentOperation) {
		t.Fatalf("stale manifest after current save: %v", err)
	}
}

// rawOperationJSON reads the persisted operation record payload straight
// out of the database, bypassing the package API, so tests can corrupt it.
func rawOperationJSON(t *testing.T, dbPath string, opID int64) []byte {
	t.Helper()
	db := rawDB(t, dbPath)
	var payload string
	if err := db.QueryRow(`SELECT record_json FROM operation_records WHERE op_id=?`, opID).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	return []byte(payload)
}

// TestLoadCurrentActivationManifestRequiresStrictCurrentOperation reproduces
// the review findings against the current-operation reader: it must route
// through the same strict current-operation validation the writer uses
// (allocation counter, decodable record, record installation binding, in one
// consistent read transaction) and must cross-check the manifest's target
// version against the operation record it claims to belong to.
func TestLoadCurrentActivationManifestRequiresStrictCurrentOperation(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s, err := Open(root, "test-install")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	snap := startCurrentOperation(t, s, "9.9.9")
	m := validManifest(snap.ID, "9.9.9")
	if _, err := s.SaveActivationManifest(ctx, m); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(root, orgComponent(), "updater", "test-install", stateFileName)
	rawOp := rawOperationJSON(t, dbPath, snap.ID)
	rawMan := rawManifestJSON(t, dbPath, snap.ID)
	db := rawDB(t, dbPath)
	restore := func() {
		t.Helper()
		if _, err := db.Exec(`UPDATE install_state SET next_op_id=?`, snap.ID+1); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`UPDATE operation_records SET record_json=? WHERE op_id=?`, string(rawOp), snap.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`UPDATE activation_manifests SET manifest_json=? WHERE op_id=?`, string(rawMan), snap.ID); err != nil {
			t.Fatal(err)
		}
	}

	// An operation whose ID was never allocated below the persisted
	// counter is corrupt, even though a manifest row exists for it.
	if _, err := db.Exec(`UPDATE install_state SET next_op_id=?`, snap.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.LoadCurrentActivationManifest(ctx); !errors.Is(err, ErrCorruptDatabase) {
		t.Fatalf("unallocated current operation accepted: %v", err)
	}
	restore()

	// A malformed current-operation record payload is corrupt.
	if _, err := db.Exec(`UPDATE operation_records SET record_json='{not json' WHERE op_id=?`, snap.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.LoadCurrentActivationManifest(ctx); !errors.Is(err, ErrCorruptDatabase) {
		t.Fatalf("malformed current operation accepted: %v", err)
	}
	restore()

	// An operation record payload bound to another installation is corrupt.
	lied := strings.Replace(string(rawOp), `"installation_id":"test-install"`, `"installation_id":"other-install"`, 1)
	if lied == string(rawOp) {
		t.Fatal("installation lie did not change the record payload")
	}
	if _, err := db.Exec(`UPDATE operation_records SET record_json=? WHERE op_id=?`, lied, snap.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.LoadCurrentActivationManifest(ctx); !errors.Is(err, ErrInstallIDMismatch) {
		t.Fatalf("mismatched current operation accepted: %v", err)
	}
	restore()

	// A manifest whose target version does not match the current
	// operation record's target version is corrupt: the reader must not
	// hand out fingerprints bound to a different version.
	tampered := strings.Replace(string(rawMan), `"target_version":"9.9.9"`, `"target_version":"8.8.8"`, 1)
	if tampered == string(rawMan) {
		t.Fatal("target tamper did not change the manifest payload")
	}
	if _, err := db.Exec(`UPDATE activation_manifests SET manifest_json=? WHERE op_id=?`, tampered, snap.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.LoadCurrentActivationManifest(ctx); !errors.Is(err, ErrCorruptDatabase) {
		t.Fatalf("target-mismatched manifest accepted: %v", err)
	}
	restore()

	// With pristine rows the strict reader loads the same manifest.
	got, rev, err := s.LoadCurrentActivationManifest(ctx)
	if err != nil || rev != 1 || !reflect.DeepEqual(got, m) {
		t.Fatalf("strict reader on pristine rows: %+v %d %v", got, rev, err)
	}
}

func TestActivationManifestBindingAndValidation(t *testing.T) {
	ctx := context.Background()
	s, err := Open(t.TempDir(), "test-install")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	snap := startCurrentOperation(t, s, "9.9.9")

	// Installation binding: another installation's manifest is refused
	// before any write.
	foreign := validManifest(snap.ID, "9.9.9")
	foreign.InstallID = "other-install"
	if _, err := s.SaveActivationManifest(ctx, foreign); !errors.Is(err, ErrInstallIDMismatch) {
		t.Fatalf("foreign installation manifest: %v", err)
	}

	// The manifest's target version must match the owning operation
	// record's target version.
	mismatched := validManifest(snap.ID, "8.8.8")
	if _, err := s.SaveActivationManifest(ctx, mismatched); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("target mismatch manifest: %v", err)
	}

	// Structural validation refusals — every mutated field must fail
	// ValidateActivationManifest with ErrInvalidRecord or
	// ErrInstallIDMismatch.
	base := validManifest(7, "9.9.9")
	cases := []struct {
		name    string
		mutate  func(*ActivationManifest)
		wantErr error
	}{
		{"wrong schema version", func(m *ActivationManifest) { m.SchemaVersion = ActivationManifestSchemaVersion + 1 }, ErrInvalidRecord},
		{"zero schema version", func(m *ActivationManifest) { m.SchemaVersion = 0 }, ErrInvalidRecord},
		{"foreign install", func(m *ActivationManifest) { m.InstallID = "other-install" }, ErrInstallIDMismatch},
		{"empty install", func(m *ActivationManifest) { m.InstallID = "" }, ErrInstallIDMismatch},
		{"zero op", func(m *ActivationManifest) { m.OpID = 0 }, ErrInvalidRecord},
		{"negative op", func(m *ActivationManifest) { m.OpID = -3 }, ErrInvalidRecord},
		{"empty target", func(m *ActivationManifest) { m.TargetVersion = "" }, ErrInvalidRecord},
		{"path target", func(m *ActivationManifest) { m.TargetVersion = strings.Repeat("9", maxActivationVersionBytes+1) }, ErrInvalidRecord},
		{"absolute staged dir", func(m *ActivationManifest) { m.StagedVersionDir = "/tmp/staged-9.9.9" }, ErrInvalidRecord},
		{"windows staged dir", func(m *ActivationManifest) { m.StagedVersionDir = `C:\staged-9.9.9` }, ErrInvalidRecord},
		{"parent staged dir", func(m *ActivationManifest) { m.StagedVersionDir = "../staged-9.9.9" }, ErrInvalidRecord},
		{"empty staged dir", func(m *ActivationManifest) { m.StagedVersionDir = "" }, ErrInvalidRecord},
		{"uppercase artifact digest", func(m *ActivationManifest) { m.VerifiedArtifactSHA256 = strings.ToUpper(strings.Repeat("a", 64)) }, ErrInvalidRecord},
		{"short artifact digest", func(m *ActivationManifest) { m.VerifiedArtifactSHA256 = strings.Repeat("a", 63) }, ErrInvalidRecord},
		{"nonhex artifact digest", func(m *ActivationManifest) { m.VerifiedArtifactSHA256 = strings.Repeat("g", 64) }, ErrInvalidRecord},
		{"empty artifact digest", func(m *ActivationManifest) { m.VerifiedArtifactSHA256 = "" }, ErrInvalidRecord},
		{"no members", func(m *ActivationManifest) { m.Members = nil }, ErrInvalidRecord},
		{"empty member list", func(m *ActivationManifest) { m.Members = []ManifestMember{} }, ErrInvalidRecord},
		{"too many members", func(m *ActivationManifest) {
			m.Members = make([]ManifestMember, 0, maxActivationManifestMembers+1)
			for i := 0; i <= maxActivationManifestMembers; i++ {
				m.Members = append(m.Members, ManifestMember{
					Path:   fmt.Sprintf("bin/tool-%d.exe", i),
					Length: 1,
					SHA256: strings.Repeat("a", 64),
				})
			}
		}, ErrInvalidRecord},
		{"duplicate member paths", func(m *ActivationManifest) {
			m.Members = []ManifestMember{
				{Path: "cercano-agent.exe", Length: 1, SHA256: strings.Repeat("a", 64)},
				{Path: "cercano-agent.exe", Length: 2, SHA256: strings.Repeat("b", 64)},
			}
		}, ErrInvalidRecord},
		{"empty member path", func(m *ActivationManifest) {
			m.Members[0].Path = ""
		}, ErrInvalidRecord},
		{"absolute member path", func(m *ActivationManifest) {
			m.Members[0].Path = "/usr/bin/cercano-agent.exe"
		}, ErrInvalidRecord},
		{"drive member path", func(m *ActivationManifest) {
			m.Members[0].Path = "C:/cercano-agent.exe"
		}, ErrInvalidRecord},
		{"backslash member path", func(m *ActivationManifest) {
			m.Members[0].Path = `bin\cercano-agent.exe`
		}, ErrInvalidRecord},
		{"parent member path", func(m *ActivationManifest) {
			m.Members[0].Path = "../cercano-agent.exe"
		}, ErrInvalidRecord},
		{"dot member component", func(m *ActivationManifest) {
			m.Members[0].Path = "./cercano-agent.exe"
		}, ErrInvalidRecord},
		{"empty member component", func(m *ActivationManifest) {
			m.Members[0].Path = "bin//cercano-agent.exe"
		}, ErrInvalidRecord},
		{"colon member path", func(m *ActivationManifest) {
			m.Members[0].Path = "bin:cercano-agent.exe"
		}, ErrInvalidRecord},
		{"alternate-data-stream member path", func(m *ActivationManifest) {
			m.Members[0].Path = "cercano-agent.exe:hidden"
		}, ErrInvalidRecord},
		{"windows-invalid member character", func(m *ActivationManifest) {
			m.Members[0].Path = "cercano-agent*.exe"
		}, ErrInvalidRecord},
		{"control byte member path", func(m *ActivationManifest) {
			m.Members[0].Path = "bin/\x01cercano-agent.exe"
		}, ErrInvalidRecord},
		{"reserved device member path", func(m *ActivationManifest) {
			m.Members[0].Path = "CON"
		}, ErrInvalidRecord},
		{"reserved device with extension", func(m *ActivationManifest) {
			m.Members[0].Path = "aux.txt"
		}, ErrInvalidRecord},
		{"reserved device directory segment", func(m *ActivationManifest) {
			m.Members[0].Path = "nul/cercano-agent.exe"
		}, ErrInvalidRecord},
		{"superscript reserved device", func(m *ActivationManifest) {
			m.Members[0].Path = "com¹.exe"
		}, ErrInvalidRecord},
		{"trailing dot member segment", func(m *ActivationManifest) {
			m.Members[0].Path = "cercano-agent.exe."
		}, ErrInvalidRecord},
		{"trailing space member segment", func(m *ActivationManifest) {
			m.Members[0].Path = "cercano-agent "
		}, ErrInvalidRecord},
		{"trailing space directory segment", func(m *ActivationManifest) {
			m.Members[0].Path = "bin /cercano-agent.exe"
		}, ErrInvalidRecord},
		{"case-fold alias member paths", func(m *ActivationManifest) {
			m.Members = []ManifestMember{
				{Path: "cercano-agent.exe", Length: 1, SHA256: strings.Repeat("a", 64)},
				{Path: "Cercano-Agent.exe", Length: 2, SHA256: strings.Repeat("b", 64)},
			}
		}, ErrInvalidRecord},
		{"member path is also a parent directory", func(m *ActivationManifest) {
			m.Members = []ManifestMember{
				{Path: "bin", Length: 1, SHA256: strings.Repeat("a", 64)},
				{Path: "bin/cercano-agent.exe", Length: 2, SHA256: strings.Repeat("b", 64)},
			}
		}, ErrInvalidRecord},
		{"oversized member path", func(m *ActivationManifest) {
			m.Members[0].Path = strings.Repeat("a", maxManifestMemberPathBytes+1)
		}, ErrInvalidRecord},
		{"zero member length", func(m *ActivationManifest) { m.Members[0].Length = 0 }, ErrInvalidRecord},
		{"negative member length", func(m *ActivationManifest) { m.Members[0].Length = -5 }, ErrInvalidRecord},
		{"member length exceeds image limit", func(m *ActivationManifest) {
			m.Members[0].Length = imagecheck.MaxImageBytes + 1
		}, ErrInvalidRecord},
		{"uppercase member digest", func(m *ActivationManifest) {
			m.Members[0].SHA256 = strings.ToUpper(m.Members[0].SHA256)
		}, ErrInvalidRecord},
		{"short member digest", func(m *ActivationManifest) { m.Members[0].SHA256 = strings.Repeat("a", 64)[:32] }, ErrInvalidRecord},
		{"empty member digest", func(m *ActivationManifest) { m.Members[0].SHA256 = "" }, ErrInvalidRecord},
	}
	for _, tc := range cases {
		m := copyManifest(base)
		tc.mutate(&m)
		err := ValidateActivationManifest(m, "test-install")
		if !errors.Is(err, tc.wantErr) {
			t.Errorf("%s: got %v, want %v", tc.name, err, tc.wantErr)
		}
	}
	if err := ValidateActivationManifest(base, "test-install"); err != nil {
		t.Fatalf("base manifest rejected: %v", err)
	}

	// The image-verification limit itself is a legitimate boundary: a
	// member of exactly imagecheck.MaxImageBytes is accepted.
	atLimit := copyManifest(base)
	atLimit.Members[0].Length = imagecheck.MaxImageBytes
	if err := ValidateActivationManifest(atLimit, "test-install"); err != nil {
		t.Fatalf("member at the image limit rejected: %v", err)
	}
}

// copyManifest returns a manifest whose Members slice is privately owned,
// so per-case mutations cannot alias the shared base.
func copyManifest(m ActivationManifest) ActivationManifest {
	m.Members = append([]ManifestMember(nil), m.Members...)
	return m
}

func TestActivationManifestStrictDecodingRefusesCorruption(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s, err := Open(root, "test-install")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	snap := startCurrentOperation(t, s, "9.9.9")
	m := validManifest(snap.ID, "9.9.9")
	if _, err := s.SaveActivationManifest(ctx, m); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(root, orgComponent(), "updater", "test-install", stateFileName)
	raw := rawManifestJSON(t, dbPath, snap.ID)

	// Corrupt the persisted payload in ways strict decoding must refuse;
	// every variant loads as ErrCorruptDatabase (or, where the corruption
	// is an installation-identity lie, ErrInstallIDMismatch), never as a
	// usable manifest, and the original row is never rewritten by a
	// refused load.
	corruptions := []struct {
		name    string
		fixed   func() []byte
		payload func(original []byte) []byte
		rowEdit func(t *testing.T, db *sql.DB)
		wantErr error
	}{
		{"truncated", nil, func(o []byte) []byte { return o[:len(o)-5] }, nil, ErrCorruptDatabase},
		{"trailing data", nil, func(o []byte) []byte { return append(append([]byte{}, o...), []byte(`{"x":1}`)...) }, nil, ErrCorruptDatabase},
		{"duplicate key", nil, func(o []byte) []byte {
			s := strings.Replace(string(o), `{`, `{"install_id":"test-install",`, 1)
			return []byte(s)
		}, nil, ErrCorruptDatabase},
		{"unknown field", nil, func(o []byte) []byte {
			s := strings.Replace(string(o), `{`, `{"unexpected":true,`, 1)
			return []byte(s)
		}, nil, ErrCorruptDatabase},
		{"null field", nil, func(o []byte) []byte {
			s := strings.Replace(string(o), `"op_id":`+fmt.Sprint(snap.ID), `"op_id":null`, 1)
			return []byte(s)
		}, nil, ErrCorruptDatabase},
		{"missing field", nil, func(o []byte) []byte {
			s := strings.Replace(string(o), `"target_version":"9.9.9",`, ``, 1)
			return []byte(s)
		}, nil, ErrCorruptDatabase},
		{"uppercase field name", nil, func(o []byte) []byte {
			s := strings.Replace(string(o), `"op_id"`, `"OP_ID"`, 1)
			return []byte(s)
		}, nil, ErrCorruptDatabase},
		{"corrupt member path", nil, func(o []byte) []byte {
			s := strings.Replace(string(o), `"cercano-agent.exe"`, `"../cercano-agent.exe"`, 1)
			return []byte(s)
		}, nil, ErrCorruptDatabase},
		{"corrupt member digest", nil, func(o []byte) []byte {
			s := strings.Replace(string(o), strings.Repeat("f", 64), strings.Repeat("g", 64), 1)
			return []byte(s)
		}, nil, ErrCorruptDatabase},
		{"corrupt member length", nil, func(o []byte) []byte {
			s := strings.Replace(string(o), `"length":111`, `"length":-111`, 1)
			return []byte(s)
		}, nil, ErrCorruptDatabase},
		{"duplicate member key", nil, func(o []byte) []byte {
			s := strings.Replace(string(o), `"length":111`, `"length":111,"length":111`, 1)
			return []byte(s)
		}, nil, ErrCorruptDatabase},
		{"uppercase member key", nil, func(o []byte) []byte {
			s := strings.Replace(string(o), `"path":"cercano-agent.exe"`, `"PATH":"cercano-agent.exe"`, 1)
			return []byte(s)
		}, nil, ErrCorruptDatabase},
		{"null member field", nil, func(o []byte) []byte {
			s := strings.Replace(string(o), `"length":111`, `"length":null`, 1)
			return []byte(s)
		}, nil, ErrCorruptDatabase},
		{"unknown member field", nil, func(o []byte) []byte {
			s := strings.Replace(string(o), `"cercano-agent.exe","length":111`, `"cercano-agent.exe","extra":1,"length":111`, 1)
			return []byte(s)
		}, nil, ErrCorruptDatabase},
		{"nested object member value", nil, func(o []byte) []byte {
			s := strings.Replace(string(o), `"path":"cercano-agent.exe"`, `"path":{"a":1}`, 1)
			return []byte(s)
		}, nil, ErrCorruptDatabase},
		{"nested array member value", nil, func(o []byte) []byte {
			s := strings.Replace(string(o), `"length":111`, `"length":[111]`, 1)
			return []byte(s)
		}, nil, ErrCorruptDatabase},
		{"members not an array", nil, func(o []byte) []byte {
			s := strings.Replace(string(o), `"members":[`, `"members":{`, 1)
			return []byte(s)
		}, nil, ErrCorruptDatabase},
		{"member element not an object", nil, func(o []byte) []byte {
			s := strings.Replace(string(o), `"members":[`, `"members":["cercano-agent.exe",`, 1)
			return []byte(s)
		}, nil, ErrCorruptDatabase},
		{"empty members array", nil, func(o []byte) []byte {
			s := strings.Replace(string(o), `"members":[`, `"members":[]`, 1)
			return []byte(s)
		}, nil, ErrCorruptDatabase},
		{"wrong install id in payload", nil, func(o []byte) []byte {
			s := strings.Replace(string(o), `"test-install"`, `"other-install"`, 1)
			return []byte(s)
		}, nil, ErrInstallIDMismatch},
		{"wrong op id in payload", nil, func(o []byte) []byte {
			s := strings.Replace(string(o), `"op_id":`+fmt.Sprint(snap.ID), `"op_id":999`, 1)
			return []byte(s)
		}, nil, ErrCorruptDatabase},
		{"oversized payload", func() []byte {
			big := make([]byte, maxActivationManifestJSONBytes+1)
			for i := range big {
				big[i] = 'a'
			}
			return big
		}, nil, nil, ErrCorruptDatabase},
		{"row install id tampered", nil, nil, func(t *testing.T, db *sql.DB) {
			if _, err := db.Exec(`UPDATE activation_manifests SET install_id='other-install' WHERE op_id=?`, snap.ID); err != nil {
				t.Fatal(err)
			}
		}, ErrInstallIDMismatch},
		{"zero revision", nil, nil, func(t *testing.T, db *sql.DB) {
			if _, err := db.Exec(`UPDATE activation_manifests SET revision=0 WHERE op_id=?`, snap.ID); err != nil {
				t.Fatal(err)
			}
		}, ErrCorruptDatabase},
	}
	for _, tc := range corruptions {
		t.Run(tc.name, func(t *testing.T) {
			// Restore the pristine row first: corruption cases must not
			// leak into each other.
			db := rawDB(t, dbPath)
			if _, err := db.Exec(`UPDATE activation_manifests SET manifest_json=?, install_id='test-install', revision=1 WHERE op_id=?`,
				string(raw), snap.ID); err != nil {
				t.Fatal(err)
			}
			if tc.rowEdit != nil {
				tc.rowEdit(t, db)
			} else {
				var variant []byte
				if tc.fixed != nil {
					variant = tc.fixed()
				} else {
					variant = tc.payload(raw)
					if string(variant) == string(raw) {
						t.Fatal("corruption did not change the payload")
					}
				}
				if _, err := db.Exec(`UPDATE activation_manifests SET manifest_json=? WHERE op_id=?`, string(variant), snap.ID); err != nil {
					t.Fatal(err)
				}
			}
			loaded, rev, err := s.LoadActivationManifest(ctx, snap.ID)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("corrupt manifest accepted: %+v %d %v (want %v)", loaded, rev, err, tc.wantErr)
			}
			if _, _, err := s.LoadCurrentActivationManifest(ctx); !errors.Is(err, tc.wantErr) {
				t.Fatalf("corrupt manifest accepted by current reader: %v (want %v)", err, tc.wantErr)
			}
		})
	}
}

// rawManifestJSON reads the persisted manifest payload straight out of the
// database, bypassing the package API, so tests can corrupt it.
func rawManifestJSON(t *testing.T, dbPath string, opID int64) []byte {
	t.Helper()
	db := rawDB(t, dbPath)
	var payload string
	if err := db.QueryRow(`SELECT manifest_json FROM activation_manifests WHERE op_id=?`, opID).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	return []byte(payload)
}

// legacyV5Database builds a fixture that is exactly the schema-5 database
// the previous release wrote: the schema-5 DDL, application_id,
// user_version 5, the meta rows, the install counter, and one persisted
// operation record plus one persisted activation journal. The fixture
// directory lives under the test's TempDir.
func legacyV5Database(t *testing.T, root, installID string) string {
	t.Helper()
	dir := filepath.Join(root, orgComponent(), "updater", installID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dir, stateFileName)
	if err := os.WriteFile(dbPath, nil, 0600); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", sqliteURI(dbPath, "rw"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close() //nolint:errcheck // fixture teardown
	fail := func(e error) {
		_, _ = conn.ExecContext(ctx, "ROLLBACK")
		t.Fatal(e)
	}
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		fail(err)
	}
	previous := int64(SchemaVersion - 1)
	for _, q := range []string{
		schemaSQLFor(previous),
		fmt.Sprintf("PRAGMA application_id = %d;", AppID),
		fmt.Sprintf("PRAGMA user_version = %d;", previous),
	} {
		if _, err := conn.ExecContext(ctx, q); err != nil {
			fail(err)
		}
	}
	meta := `INSERT INTO state_meta (key, value) VALUES (?, ?), (?, ?)`
	if _, err := conn.ExecContext(ctx, meta, metaKeySchemaVersion, fmt.Sprint(previous), metaKeyInstallID, installID); err != nil {
		fail(err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO install_state (install_id, next_op_id) VALUES (?, 2)`, installID); err != nil {
		fail(err)
	}
	// One committed legacy operation record (identifier 1, counter now 2).
	model := operation.NewStore()
	snap, err := model.Start(installID, "1.2.3")
	if err != nil {
		fail(err)
	}
	rec := snap.Record()
	rec.ID = 1
	payload, err := json.Marshal(rec)
	if err != nil {
		fail(err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO operation_records (op_id, install_id, revision, record_json) VALUES (1, ?, 1, ?)`,
		installID, string(payload)); err != nil {
		fail(err)
	}
	// One committed legacy activation journal.
	j := journalFor(1, "1.2.3")
	journalPayload, err := json.Marshal(j)
	if err != nil {
		fail(err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO activation_journals (op_id, install_id, revision, journal_json) VALUES (1, ?, 1, ?)`,
		installID, string(journalPayload)); err != nil {
		fail(err)
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		fail(err)
	}
	return dbPath
}

func TestActivationManifestMigrationPreservesLegacyV5(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	dbPath := legacyV5Database(t, root, "test-install")
	legacyJournal := journalFor(1, "1.2.3")

	s, err := Open(root, "test-install")
	if err != nil {
		t.Fatalf("upgrade open failed: %v", err)
	}
	defer s.Close()

	if v := rawVersion(t, dbPath); v != SchemaVersion {
		t.Fatalf("user_version after upgrade = %d", v)
	}
	// Every legacy record survives the upgrade untouched.
	loaded, rev, err := s.LoadOperationRecord(ctx, 1)
	if err != nil || rev != 1 || loaded.ID != 1 || loaded.TargetVersion != "1.2.3" {
		t.Fatalf("legacy operation record lost: %+v %d %v", loaded, rev, err)
	}
	j, jrev, err := s.LoadActivationJournal(ctx, 1)
	if err != nil || jrev != 1 || j != legacyJournal {
		t.Fatalf("legacy journal lost: %+v %d %v", j, jrev, err)
	}
	id, err := s.AllocateOperationID(ctx)
	if err != nil || id != 2 {
		t.Fatalf("counter after upgrade = %d %v", id, err)
	}
	// The upgraded installation immediately persists manifests against the
	// migrated CURRENT operation.
	m := validManifest(1, "1.2.3")
	if mrev, err := s.SaveActivationManifest(ctx, m); err != nil || mrev != 1 {
		t.Fatalf("post-upgrade manifest save %d %v", mrev, err)
	}
	if got, grev, err := s.LoadCurrentActivationManifest(ctx); err != nil || grev != 1 || !reflect.DeepEqual(got, m) {
		t.Fatalf("post-upgrade manifest load: %+v %d %v", got, grev, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	// Reopening the upgraded database is idempotent and keeps the row.
	s2, err := Open(root, "test-install")
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if v := rawVersion(t, dbPath); v != SchemaVersion {
		t.Fatalf("user_version after reopen = %d", v)
	}
	if _, rev, err = s2.LoadActivationManifest(ctx, 1); err != nil || rev != 1 {
		t.Fatalf("manifest lost after reopen: %v", err)
	}
}

func TestActivationManifestMigrationFailurePreservesV5AndRollsBack(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	dbPath := legacyV5Database(t, root, "test-install")

	migrationFault = func() error { return errors.New("injected manifest-table migration failure") }
	if _, err := Open(root, "test-install"); err == nil {
		t.Fatal("faulted migration unexpectedly succeeded")
	}
	migrationFault = nil

	// The database is logically still an untouched, complete schema-5
	// database: same version, no partial table, legacy rows intact.
	if v := rawVersion(t, dbPath); v != int64(SchemaVersion-1) {
		t.Fatalf("user_version after failed migration = %d", v)
	}
	db := rawDB(t, dbPath)
	var tables int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='activation_manifests'`).Scan(&tables); err != nil || tables != 0 {
		t.Fatalf("partial activation_manifests survived rollback: %d %v", tables, err)
	}
	var meta string
	if err := db.QueryRow(`SELECT value FROM state_meta WHERE key = ?`, metaKeySchemaVersion).Scan(&meta); err != nil || meta != fmt.Sprint(int64(SchemaVersion-1)) {
		t.Fatalf("meta schema_version after failed migration = %q %v", meta, err)
	}

	// With the fault cleared the same database upgrades cleanly.
	s, err := Open(root, "test-install")
	if err != nil {
		t.Fatalf("retry open after rollback failed: %v", err)
	}
	defer s.Close()
	if v := rawVersion(t, dbPath); v != SchemaVersion {
		t.Fatalf("user_version after retry = %d", v)
	}
	if _, _, err = s.LoadOperationRecord(ctx, 1); err != nil {
		t.Fatalf("legacy record lost after retry: %v", err)
	}
	if _, _, err = s.LoadActivationJournal(ctx, 1); err != nil {
		t.Fatalf("legacy journal lost after retry: %v", err)
	}
	if _, _, err = s.LoadActivationManifest(ctx, 1); !errors.Is(err, ErrRecordNotFound) {
		t.Fatalf("manifest must be absent after migration: %v", err)
	}
}
