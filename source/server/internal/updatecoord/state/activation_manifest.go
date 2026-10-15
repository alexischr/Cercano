package state

// Durable activation manifests (typed record + strict SQLite storage API).
//
// The user approved persisting the verified per-executable manifest as
// installation-bound recovery facts for versioned-child startup
// verification. This file is that manifest's record and storage API ONLY:
// no filesystem writes, no activation or restart, no image verification,
// no command execution, and no self-update. Launcher handoff values remain
// LOCATORS, not authority: a child must obtain expected image fingerprints
// independently from protected installation state, and the read APIs here
// exist precisely so an independent child can do that without trusting any
// handoff value.
//
// Contract:
//
//   - The operation record remains the lifecycle authority. A manifest row
//     is written only for an allocated, existing operation that is the
//     installation's CURRENT operation and whose target version matches —
//     checked inside the same transaction as the write — so no orphan,
//     stale, or superseded writer can attach a manifest.
//   - A manifest row is written EXACTLY ONCE. It is the captured
//     verification-time fact for one operation; re-saves (identical or
//     mutated) are refused with ErrStaleRevision and nothing written. There
//     is no update path, no progression, and no deletion through this API.
//   - The persisted payload carries the verified agent/client member paths
//     (bounded relative archive-internal slash paths), byte lengths, and
//     SHA-256 fingerprints bound to the installation, operation, selected
//     version directory identifier, and verified archive digest.
//   - Binding to the activation journal's staged facts (staged directory
//     and artifact digest) is deliberately NOT enforced in this storage
//     slice: the activation executor must reconcile the manifest against
//     the operation's journal before acting on either. This API stores
//     what was captured; it does not authorize any use of it.
//   - Corrupt rows are refused as ErrCorruptDatabase/ErrInstallIDMismatch,
//     never reset, never treated as empty.
//
// Storage follows the same strict discipline as activation journals and
// operation records: BEGIN IMMEDIATE writes, per-row revision, bounded
// canonical JSON payloads, exact install-id binding, and schema-versioned
// additive migration (schema version 6).

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"cercano/source/server/internal/updatecoord/archivecheck"
	"cercano/source/server/internal/updatecoord/imagecheck"
)

// ActivationManifestSchemaVersion is the schema version ActivationManifest
// currently models. Stored payloads with any other schema version are
// refused, never coerced.
const ActivationManifestSchemaVersion = 1

// maxActivationManifestJSONBytes bounds the persisted manifest payload
// (looser than the journal bound because it carries per-member entries),
// enforced on write before storage.
const maxActivationManifestJSONBytes = 64 * 1024

// maxActivationManifestMembers bounds the verified member set the manifest
// may persist; a real agent/client release has a handful of executables.
const maxActivationManifestMembers = 64

// maxManifestMemberPathBytes bounds each member's archive-internal path.
const maxManifestMemberPathBytes = 256

// ManifestMember is one verified executable image inside the staged
// version directory, exactly as archivecheck captured it: the
// archive-internal slash path, the byte length, and the SHA-256 (64
// lowercase hex) of the verified image. A member is data, never authority:
// authorization flows through the operation record, journal, and policy.
type ManifestMember struct {
	// Path is the member's archive-internal slash path only — relative,
	// never absolute, never a Windows drive or backslash path, never a
	// `..` or `.` component. It is interpreted only by the trusted
	// executor joining it to its OWN trusted version-directory root.
	Path string `json:"path"`
	// Length is the verified image's byte length, captured at
	// verification time, never re-derived.
	Length int64 `json:"length"`
	// SHA256 is the verified image's lowercase-hex SHA-256 fingerprint,
	// captured at verification time, never re-derived.
	SHA256 string `json:"sha256"`
}

// ActivationManifest is the typed, strict-JSON, installation-bound record
// of the verified per-executable manifest for exactly ONE operation. It is
// a recovery fact for versioned-child startup verification, not an
// authorization grant. Every field is immutable: the manifest is written
// once and never updated.
type ActivationManifest struct {
	// SchemaVersion must equal ActivationManifestSchemaVersion.
	SchemaVersion int `json:"schema_version"`
	// InstallID is the installation this manifest is bound to.
	InstallID string `json:"install_id"`
	// OpID is the existing, allocated operation this manifest belongs to;
	// at write time it must be the installation's CURRENT operation.
	OpID int64 `json:"op_id"`
	// TargetVersion is the version the operation activates; it must equal
	// the owning operation record's target version.
	TargetVersion string `json:"target_version"`
	// StagedVersionDir identifies the staged version directory as a
	// RELATIVE identifier only — never absolute, never a Windows drive
	// path, never a backslash path, never a `..` or `.` component. It is
	// interpreted by the trusted executor; this package never opens it.
	StagedVersionDir string `json:"staged_version_dir"`
	// VerifiedArtifactSHA256 is the SHA-256 (64 lowercase hex) of the
	// verified archive the staged directory was staged from, captured at
	// verification time, never re-derived.
	VerifiedArtifactSHA256 string `json:"verified_artifact_sha256"`
	// Members are the verified executable images the child is expected to
	// verify itself against, one entry per verified regular-file member.
	// The list must be nonempty, contain no duplicate paths, and stay
	// within the bounded member/path rules.
	Members []ManifestMember `json:"members"`
}

// SaveActivationManifest validates m against the manifest schema, this
// store's installation, and the installation's CURRENT operation record —
// then persists it as a fresh row.
//
// It is a strict CREATE-ONLY primitive: a manifest is written exactly once
// per operation. Any second save for the same operation — identical or
// mutated — is refused with ErrStaleRevision and nothing is written; there
// is no update, no progression, and no deletion path through this API.
//
// Authority and anti-orphan rules, all inside ONE BEGIN IMMEDIATE
// transaction with the write itself:
//
//   - m.InstallID must equal this store's installation
//     (ErrInstallIDMismatch).
//   - m.OpID must name the installation's CURRENT operation (highest
//     persisted ID); nonexistent, unallocated, or superseded operations
//     are refused with ErrRecordNotFound or ErrNotCurrentOperation before
//     any write.
//   - m.TargetVersion must equal the owning operation record's target
//     version (ErrInvalidRecord).
//
// Validation is structural only: this slice performs no filesystem change,
// no image verification, and no process action, and it does not reconcile
// the manifest against the activation journal (the activation executor
// must do that before acting on either).
func (s *Store) SaveActivationManifest(ctx context.Context, m ActivationManifest) (int64, error) {
	if err := ValidateActivationManifest(m, s.installID); err != nil {
		return 0, err
	}
	payload, err := json.Marshal(m)
	if err != nil {
		return 0, fmt.Errorf("%w: marshal: %v", ErrInvalidRecord, err)
	}
	if len(payload) > maxActivationManifestJSONBytes {
		return 0, fmt.Errorf("%w: manifest payload exceeds %d bytes", ErrInvalidRecord, maxActivationManifestJSONBytes)
	}
	var revision int64
	err = s.inWriteTx(ctx, func(ctx context.Context, conn *sql.Conn) error {
		rev, err := s.saveActivationManifestConn(ctx, conn, m, payload)
		if err != nil {
			return err
		}
		revision = rev
		return nil
	})
	if err != nil {
		return 0, err
	}
	return revision, nil
}

// saveActivationManifestConn is the conn-scoped manifest-write primitive.
// It MUST be called inside a caller-owned BEGIN IMMEDIATE transaction; the
// current-operation authority check shares that transaction with the write.
func (s *Store) saveActivationManifestConn(ctx context.Context, conn *sql.Conn, m ActivationManifest, payload []byte) (int64, error) {
	if s.fault != nil {
		if ferr := s.fault(); ferr != nil {
			return 0, ferr
		}
	}
	// Authority: the manifest belongs to the EXISTING, allocated, CURRENT
	// operation, loaded through the same strict, binding-checked path the
	// adapter uses; a manifest can never name an operation that was never
	// allocated or persisted.
	rec, _, found, err := s.loadCurrentOperation(ctx, conn)
	if err != nil {
		return 0, err
	}
	if !found {
		return 0, fmt.Errorf("%w: no operation exists to own an activation manifest", ErrRecordNotFound)
	}
	if m.OpID != rec.ID {
		return 0, fmt.Errorf("%w: manifest names operation %d, current operation is %d", ErrNotCurrentOperation, m.OpID, rec.ID)
	}
	if m.TargetVersion != rec.TargetVersion {
		return 0, fmt.Errorf("%w: manifest target %q does not match operation %d target %q", ErrInvalidRecord, m.TargetVersion, rec.ID, rec.TargetVersion)
	}
	// Create-only: any existing row for this operation refuses the save,
	// identical or not. The manifest is a captured fact, never rewritten.
	var count int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM activation_manifests WHERE op_id=?`, m.OpID).Scan(&count); err != nil {
		return 0, err
	}
	if count != 0 {
		return 0, ErrStaleRevision
	}
	if _, err := conn.ExecContext(ctx,
		`INSERT INTO activation_manifests (op_id, install_id, revision, manifest_json) VALUES (?, ?, 1, ?)`,
		m.OpID, s.installID, string(payload)); err != nil {
		return 0, fmt.Errorf("state: insert activation manifest: %w", err)
	}
	return 1, nil
}

// LoadActivationManifest loads the persisted activation manifest of the
// given operation together with its revision. A missing manifest is
// ErrRecordNotFound. A row that fails strict decoding or validation —
// malformed JSON, duplicate keys, unknown fields, null-valued fields,
// unknown schema version, or an identity that does not match its row and
// this store's installation — is ErrCorruptDatabase or
// ErrInstallIDMismatch: corrupt data is rejected, never treated as empty,
// never silently reset. It is a read-only path that performs no write and
// takes no write lock; it deliberately does NOT require the operation to
// still be current (recovery and reconciliation of a superseded operation
// may need to read its manifest).
func (s *Store) LoadActivationManifest(ctx context.Context, opID int64) (ActivationManifest, int64, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	return s.loadActivationManifestConn(ctx, s.db, opID)
}

// loadActivationManifestConn is the querier-scoped manifest-read primitive:
// one bounded, consistent snapshot of the row, strictly decoded and
// validated. It is shared by the by-ID reader (db-backed) and the
// current-operation reader (tx-backed) so both enforce identical rules.
func (s *Store) loadActivationManifestConn(ctx context.Context, q rowQuerier, opID int64) (ActivationManifest, int64, error) {
	if opID <= 0 {
		return ActivationManifest{}, 0, fmt.Errorf("%w: operation id must be positive", ErrInvalidRecord)
	}
	var rowInstall, payload string
	var revision int64
	err := q.QueryRowContext(ctx,
		`SELECT install_id, revision, manifest_json FROM activation_manifests WHERE op_id = ?`, opID).Scan(&rowInstall, &revision, &payload)
	if errors.Is(err, sql.ErrNoRows) {
		return ActivationManifest{}, 0, fmt.Errorf("%w: activation manifest %d", ErrRecordNotFound, opID)
	}
	if err != nil {
		return ActivationManifest{}, 0, fmt.Errorf("state: load activation manifest: %w", err)
	}
	if rowInstall != s.installID {
		return ActivationManifest{}, 0, fmt.Errorf("%w: manifest row names installation %q, store is %q", ErrInstallIDMismatch, rowInstall, s.installID)
	}
	if revision < 1 {
		return ActivationManifest{}, 0, fmt.Errorf("%w: activation manifest %d at revision %d", ErrCorruptDatabase, opID, revision)
	}
	m, err := decodeActivationManifest([]byte(payload))
	if err != nil {
		return ActivationManifest{}, 0, fmt.Errorf("%w: activation manifest %d: %v", ErrCorruptDatabase, opID, err)
	}
	if m.InstallID != s.installID {
		return ActivationManifest{}, 0, fmt.Errorf("%w: activation manifest %d is bound to a different installation", ErrInstallIDMismatch, opID)
	}
	if m.OpID != opID {
		return ActivationManifest{}, 0, fmt.Errorf("%w: activation manifest %d names operation %d", ErrCorruptDatabase, opID, m.OpID)
	}
	return m, revision, nil
}

// LoadCurrentActivationManifest loads the current operation's manifest using
// strict operation validation and one read transaction. Current does NOT mean
// selected or installed. Startup consumers must instead reconcile selection,
// journal and operation (LoadCompletedSelectionManifest for completed installs).
func (s *Store) LoadCurrentActivationManifest(ctx context.Context) (ActivationManifest, int64, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	var (
		m        ActivationManifest
		revision int64
	)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return ActivationManifest{}, 0, fmt.Errorf("state: begin manifest read: %w", err)
	}
	defer tx.Rollback()
	// Currency is established by the strict shared loader, not by a bare
	// "highest op_id" probe: unallocated, malformed, or
	// installation-mismatched current rows are ErrCorruptDatabase /
	// ErrInstallIDMismatch, and a superseded-but-existing operation is
	// exactly what this loader returns as current.
	rec, _, found, err := s.loadCurrentOperation(ctx, tx)
	if err != nil {
		return ActivationManifest{}, 0, err
	}
	if !found {
		return ActivationManifest{}, 0, fmt.Errorf("%w: no operation exists to own an activation manifest", ErrRecordNotFound)
	}
	m, revision, err = s.loadActivationManifestConn(ctx, tx, rec.ID)
	if err != nil {
		return ActivationManifest{}, 0, err
	}
	// The manifest must still bind to the operation it claims: its target
	// version must match the record's, not just its operation ID. A
	// mismatch is corrupt persisted data.
	if m.TargetVersion != rec.TargetVersion {
		return ActivationManifest{}, 0, fmt.Errorf("%w: activation manifest %d target %q does not match operation %d target %q", ErrCorruptDatabase, rec.ID, m.TargetVersion, rec.ID, rec.TargetVersion)
	}
	if err := tx.Commit(); err != nil {
		return ActivationManifest{}, 0, fmt.Errorf("state: commit manifest read: %w", err)
	}
	return m, revision, nil
}

// ValidateActivationManifest performs the full structural validation of a
// manifest against the store's installation: exact schema version, positive
// operation ID, installation binding, bounded valid target version, a
// RELATIVE staged-directory identifier, a lowercase-hex SHA-256 verified
// artifact, and a bounded, nonempty member set of distinct relative paths
// with positive lengths and lowercase-hex SHA-256 fingerprints. It is
// exported so an independent child verifier can validate a loaded manifest
// against its own recorded installation binding without duplicating these
// rules.
func ValidateActivationManifest(m ActivationManifest, installID string) error {
	if m.SchemaVersion != ActivationManifestSchemaVersion {
		return fmt.Errorf("%w: schema version %d, want %d", ErrInvalidRecord, m.SchemaVersion, ActivationManifestSchemaVersion)
	}
	if m.InstallID != installID {
		return fmt.Errorf("%w: manifest names installation %q, store is %q", ErrInstallIDMismatch, m.InstallID, installID)
	}
	if m.OpID <= 0 {
		return fmt.Errorf("%w: operation id must be positive", ErrInvalidRecord)
	}
	if err := ValidateVersionString(m.TargetVersion); err != nil {
		return fmt.Errorf("%w: target version: %v", ErrInvalidRecord, err)
	}
	if err := ValidateStagedRelativeIdentifier(m.StagedVersionDir); err != nil {
		return err
	}
	if !IsLowerHexSHA256(m.VerifiedArtifactSHA256) {
		return fmt.Errorf("%w: verified artifact digest must be a 64-character lowercase hex SHA-256", ErrInvalidRecord)
	}
	if len(m.Members) == 0 {
		return fmt.Errorf("%w: manifest must record at least one verified member", ErrInvalidRecord)
	}
	if len(m.Members) > maxActivationManifestMembers {
		return fmt.Errorf("%w: manifest records %d members, at most %d", ErrInvalidRecord, len(m.Members), maxActivationManifestMembers)
	}
	paths := make([]string, 0, len(m.Members))
	for _, member := range m.Members {
		if err := validateManifestMemberPath(member.Path); err != nil {
			return err
		}
		paths = append(paths, member.Path)
		if member.Length <= 0 {
			return fmt.Errorf("%w: member %q must record a positive byte length", ErrInvalidRecord, member.Path)
		}
		// A member can never be larger than the verified image it was
		// extracted from; the same ceiling image verification enforces.
		if member.Length > imagecheck.MaxImageBytes {
			return fmt.Errorf("%w: member %q length %d exceeds the image limit", ErrInvalidRecord, member.Path, member.Length)
		}
		if !IsLowerHexSHA256(member.SHA256) {
			return fmt.Errorf("%w: member %q fingerprint must be a 64-character lowercase hex SHA-256", ErrInvalidRecord, member.Path)
		}
	}
	// The member set forms ONE portable namespace: no two members may fold
	// to the same case key (including Unicode folds strings.ToLower misses),
	// no member may be used as both a file and a directory, and no member
	// may collide with another member's implicit parent directory — the
	// exact namespace contract the archive layout validator enforced.
	if err := archivecheck.ValidatePortableNamespace(paths); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidRecord, err)
	}
	return nil
}

// validateManifestMemberPath applies the archive-internal portable path
// rules a member carries, via the exported archivecheck validator. The
// manifest never carries a path that could escape the executor's trusted
// version-directory root by itself, and every member name must satisfy the
// same portable contract the archive gate enforced before staging: valid
// UTF-8, bounded, relative, slash-separated, no NUL, no backslash, no
// colon, no Windows-invalid characters, no control bytes, no `.` or `..`
// component, no Windows reserved device name in any segment (including
// superscript forms), and no segment ending in a dot or space.
func validateManifestMemberPath(path string) error {
	if path == "" {
		return fmt.Errorf("%w: member path must not be empty", ErrInvalidRecord)
	}
	if len(path) > maxManifestMemberPathBytes {
		return fmt.Errorf("%w: member path exceeds %d bytes", ErrInvalidRecord, maxManifestMemberPathBytes)
	}
	if err := archivecheck.ValidatePortableName(path); err != nil {
		return fmt.Errorf("%w: member path: %v", ErrInvalidRecord, err)
	}
	return nil
}

// decodeActivationManifest strictly decodes a persisted manifest payload:
// bounded, valid UTF-8, exactly one JSON object, no duplicate keys (top
// level and inside every member object), no unknown fields, no null-valued
// fields, no trailing data, then full validation. Corrupt rows are errors,
// never empty manifests.
func decodeActivationManifest(data []byte) (ActivationManifest, error) {
	var m ActivationManifest
	if len(data) > maxActivationManifestJSONBytes || len(data) == 0 || !utf8.Valid(data) {
		return m, ErrInvalidRecord
	}
	// Manifest fields are flat canonical names: reject duplicate/case
	// aliases before encoding/json can silently pick a last value. Member
	// objects are scanned element-wise with the same rule.
	scan := json.NewDecoder(bytes.NewReader(data))
	tok, err := scan.Token()
	if err != nil || tok != json.Delim('{') {
		return m, ErrInvalidRecord
	}
	seen := map[string]bool{}
	requiredFields := map[string]bool{
		"schema_version":           true,
		"install_id":               true,
		"op_id":                    true,
		"target_version":           true,
		"staged_version_dir":       true,
		"verified_artifact_sha256": true,
		"members":                  true,
	}
	for scan.More() {
		tok, err = scan.Token()
		if err != nil {
			return m, ErrInvalidRecord
		}
		key, ok := tok.(string)
		if !ok || key == "" || key != strings.ToLower(key) || seen[key] {
			return m, ErrInvalidRecord
		}
		seen[key] = true
		delete(requiredFields, key)
		if key == "members" {
			if err := scanManifestMembersArray(scan); err != nil {
				return m, err
			}
			continue
		}
		var value json.RawMessage
		if err = scan.Decode(&value); err != nil {
			return m, ErrInvalidRecord
		}
		// Reject null values for required fields — they decode to the
		// empty/zero values validation would otherwise miss on strings.
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return m, fmt.Errorf("%w: null value not allowed for field %q", ErrInvalidRecord, key)
		}
	}
	// Enforce exact canonical required field presence before decode
	if len(requiredFields) > 0 {
		return m, fmt.Errorf("%w: missing required manifest fields", ErrInvalidRecord)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return ActivationManifest{}, fmt.Errorf("malformed JSON: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		if err != nil {
			return ActivationManifest{}, fmt.Errorf("trailing data: %w", err)
		}
		return ActivationManifest{}, errors.New("trailing data")
	}
	if err := ValidateActivationManifest(m, m.InstallID); err != nil {
		return ActivationManifest{}, err
	}
	return m, nil
}

// scanManifestMembersArray consumes exactly one JSON array of member
// objects from the streaming decoder, rejecting duplicate member keys,
// non-canonical member keys, non-object elements, and null member values.
// The element values themselves are decoded again by the strict full
// decode; this scan exists so structural aliasing cannot hide inside a
// member object.
func scanManifestMembersArray(scan *json.Decoder) error {
	tok, err := scan.Token()
	if err != nil || tok != json.Delim('[') {
		return ErrInvalidRecord
	}
	for scan.More() {
		tok, err = scan.Token()
		if err != nil || tok != json.Delim('{') {
			return ErrInvalidRecord
		}
		memberSeen := map[string]bool{}
		for scan.More() {
			tok, err = scan.Token()
			if err != nil {
				return ErrInvalidRecord
			}
			mkey, ok := tok.(string)
			if !ok || mkey == "" || mkey != strings.ToLower(mkey) || memberSeen[mkey] {
				return ErrInvalidRecord
			}
			memberSeen[mkey] = true
			var value json.RawMessage
			if err = scan.Decode(&value); err != nil {
				return ErrInvalidRecord
			}
			if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return fmt.Errorf("%w: null value not allowed for member field %q", ErrInvalidRecord, mkey)
			}
		}
		tok, err = scan.Token()
		if err != nil || tok != json.Delim('}') {
			return ErrInvalidRecord
		}
	}
	tok, err = scan.Token()
	if err != nil || tok != json.Delim(']') {
		return ErrInvalidRecord
	}
	return nil
}
