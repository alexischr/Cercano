package state

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"

	"cercano/source/server/internal/updatecoord/operation"
)

// SelectedVersion describes observed selection metadata, not an executable path
// or authority to run a program. Callers validate and re-read the selection file.
type SelectedVersion struct {
	Version, Directory, ArtifactSHA256 string
	Generation                         int64
}

// CompletedSelectionOperation finds the completed operation for this exact
// selection, including when a newer check has become the current operation.
// Only SELECTs run, in one read transaction; nothing is created or migrated.
// More than one matching journal, corrupt state or incomplete health/completion
// refuses a notice. A matching journal is installation bookkeeping, not a new
// cryptographic trust root or permission to restart a client.
func (s *Store) CompletedSelectionOperation(ctx context.Context, target SelectedVersion) (int64, bool, error) {
	if ctx == nil || ValidateVersionString(target.Version) != nil || ValidateStagedRelativeIdentifier(target.Directory) != nil || !IsLowerHexSHA256(target.ArtifactSHA256) || target.Generation < 1 {
		return 0, false, ErrInvalidRecord
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback()
	id, found, err := s.completedSelectionOperation(ctx, tx, target)
	if err != nil || !found {
		return 0, false, err
	}
	if err = tx.Commit(); err != nil {
		return 0, false, err
	}
	return id, true, nil
}

func (s *Store) completedSelectionOperation(ctx context.Context, tx *sql.Tx, target SelectedVersion) (int64, bool, error) {
	// JSON predicates bound result allocation. Malformed JSON raises an error;
	// row/payload identities and types are independently checked after decoding.
	rows, err := tx.QueryContext(ctx, `SELECT op_id,install_id,revision,journal_json FROM activation_journals
 WHERE json_extract(journal_json,'$.target_version')=?
 AND json_extract(journal_json,'$.staged_version_dir')=?
 AND json_extract(journal_json,'$.verified_artifact_sha256')=?
 AND json_extract(journal_json,'$.prior_selection_generation')=? LIMIT 2`, target.Version, target.Directory, target.ArtifactSHA256, target.Generation-1)
	if err != nil {
		return 0, false, err
	}
	var found ActivationJournal
	count := 0
	for rows.Next() {
		var id, rev int64
		var install, payload string
		if err = rows.Scan(&id, &install, &rev, &payload); err != nil {
			rows.Close()
			return 0, false, err
		}
		j, e := decodeActivationJournal([]byte(payload))
		if e != nil || rev < 1 || id != j.OpID || install != s.installID || j.InstallID != s.installID || j.PriorSelectionGeneration == math.MaxInt64 {
			rows.Close()
			return 0, false, ErrCorruptDatabase
		}
		if j.TargetVersion != target.Version || j.StagedVersionDir != target.Directory || j.VerifiedArtifactSHA256 != target.ArtifactSHA256 || j.PriorSelectionGeneration+1 != target.Generation {
			rows.Close()
			return 0, false, ErrCorruptDatabase
		}
		found = j
		count++
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, false, err
	}
	if count == 0 {
		return 0, false, nil
	}
	if count != 1 {
		return 0, false, fmt.Errorf("%w: ambiguous installed selection", ErrCorruptDatabase)
	}
	if found.Checkpoint != JournalComplete {
		return 0, false, nil
	}
	var install, payload string
	var revision int64
	err = tx.QueryRowContext(ctx, `SELECT install_id,revision,record_json FROM operation_records WHERE op_id=?`, found.OpID).Scan(&install, &revision, &payload)
	if err != nil {
		return 0, false, fmt.Errorf("%w: installed operation: %v", ErrCorruptDatabase, err)
	}
	record, e := decodeOperationRecord([]byte(payload))
	if e != nil || revision < 1 || install != s.installID || record.InstallationID != s.installID || record.ID != found.OpID || record.TargetVersion != found.TargetVersion {
		return 0, false, ErrCorruptDatabase
	}
	if record.State != operation.StateComplete || !record.HealthVerified || record.RecoveryNeeded || record.RecoveryRequested {
		return 0, false, nil
	}
	return found.OpID, true, nil
}

// LoadCandidateSelectionManifest resolves the manifest that authorizes the
// CURRENT operation's candidate child startup, exactly as the user approved:
// the current, allocated operation must be in the restarting or health-check
// lifecycle and its activation journal must sit at the Selected checkpoint,
// with the journal's target facts and prior+1 selection generation matching
// the caller's selection. Journal, operation and manifest bindings are
// checked in one read transaction, sharing the strict loaders of the
// completed path; the persisted manifest is immutable (written once) and
// never re-derived from disk. Anything outside that exact startup window —
// other lifecycle states, other journal checkpoints, no operation, no
// journal, no manifest, unallocated records, or fact/generation mismatches
// between the journal, operation and selection — is refused as
// ErrRecordNotFound (nothing selected for startup) or ErrCorruptDatabase
// (inconsistent persisted facts), never treated as a completed install.
// This is not a completed-install API: completed selections keep using
// LoadCompletedSelectionManifest, which still refuses candidate states.
func (s *Store) LoadCandidateSelectionManifest(ctx context.Context, target SelectedVersion) (ActivationManifest, error) {
	if ctx == nil || ValidateVersionString(target.Version) != nil || ValidateStagedRelativeIdentifier(target.Directory) != nil || !IsLowerHexSHA256(target.ArtifactSHA256) || target.Generation < 1 {
		return ActivationManifest{}, ErrInvalidRecord
	}
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return ActivationManifest{}, err
	}
	defer tx.Rollback()
	rec, _, found, err := s.loadCurrentOperation(ctx, tx)
	if err != nil {
		return ActivationManifest{}, err
	}
	if !found {
		return ActivationManifest{}, fmt.Errorf("%w: no allocated operation to authorize candidate startup", ErrRecordNotFound)
	}
	if rec.State != operation.StateRestarting && rec.State != operation.StateHealthCheck {
		return ActivationManifest{}, fmt.Errorf("%w: operation %d is %q, not a candidate startup lifecycle", ErrRecordNotFound, rec.ID, rec.State)
	}
	if rec.TargetVersion != target.Version {
		return ActivationManifest{}, fmt.Errorf("%w: current operation %d targets %q, selection names %q", ErrCorruptDatabase, rec.ID, rec.TargetVersion, target.Version)
	}
	var rowInstall, payload string
	var revision int64
	err = tx.QueryRowContext(ctx,
		`SELECT install_id, revision, journal_json FROM activation_journals WHERE op_id = ?`, rec.ID).Scan(&rowInstall, &revision, &payload)
	if errors.Is(err, sql.ErrNoRows) {
		return ActivationManifest{}, fmt.Errorf("%w: activation journal %d", ErrRecordNotFound, rec.ID)
	}
	if err != nil {
		return ActivationManifest{}, fmt.Errorf("state: load activation journal: %w", err)
	}
	if rowInstall != s.installID {
		return ActivationManifest{}, fmt.Errorf("%w: journal row names installation %q, store is %q", ErrInstallIDMismatch, rowInstall, s.installID)
	}
	if revision < 1 {
		return ActivationManifest{}, fmt.Errorf("%w: activation journal %d at revision %d", ErrCorruptDatabase, rec.ID, revision)
	}
	j, err := decodeActivationJournal([]byte(payload))
	if err != nil {
		return ActivationManifest{}, fmt.Errorf("%w: activation journal %d: %v", ErrCorruptDatabase, rec.ID, err)
	}
	if j.InstallID != s.installID {
		return ActivationManifest{}, fmt.Errorf("%w: activation journal %d is bound to a different installation", ErrInstallIDMismatch, rec.ID)
	}
	if j.OpID != rec.ID {
		return ActivationManifest{}, fmt.Errorf("%w: activation journal %d names operation %d", ErrCorruptDatabase, rec.ID, j.OpID)
	}
	if j.Checkpoint != JournalSelected {
		return ActivationManifest{}, fmt.Errorf("%w: operation %d journal is at checkpoint %q, not the approved selected checkpoint", ErrRecordNotFound, rec.ID, j.Checkpoint)
	}
	if j.TargetVersion != target.Version || j.StagedVersionDir != target.Directory || j.VerifiedArtifactSHA256 != target.ArtifactSHA256 {
		return ActivationManifest{}, fmt.Errorf("%w: selected journal %d facts do not match the selection", ErrCorruptDatabase, rec.ID)
	}
	if j.PriorSelectionGeneration == math.MaxInt64 || j.PriorSelectionGeneration+1 != target.Generation {
		return ActivationManifest{}, fmt.Errorf("%w: selected journal %d generation %d does not immediately precede selection generation %d", ErrCorruptDatabase, rec.ID, j.PriorSelectionGeneration, target.Generation)
	}
	m, _, err := s.loadActivationManifestConn(ctx, tx, rec.ID)
	if err != nil {
		return ActivationManifest{}, err
	}
	if m.TargetVersion != target.Version || m.StagedVersionDir != target.Directory || m.VerifiedArtifactSHA256 != target.ArtifactSHA256 {
		return ActivationManifest{}, ErrCorruptDatabase
	}
	if err := tx.Commit(); err != nil {
		return ActivationManifest{}, err
	}
	return m, nil
}

// LoadCompletedSelectionManifest resolves fingerprints for an exact completed,
// health-verified selection, even while a newer operation is pending. Journal,
// operation and manifest bindings are checked in one read transaction. This is
// not a candidate-startup API: selected-but-unverified images are refused.
// Missing legacy manifests are unavailable, never synthesized from disk.
func (s *Store) LoadCompletedSelectionManifest(ctx context.Context, target SelectedVersion) (ActivationManifest, error) {
	if ctx == nil || ValidateVersionString(target.Version) != nil || ValidateStagedRelativeIdentifier(target.Directory) != nil || !IsLowerHexSHA256(target.ArtifactSHA256) || target.Generation < 1 {
		return ActivationManifest{}, ErrInvalidRecord
	}
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return ActivationManifest{}, err
	}
	defer tx.Rollback()
	id, found, err := s.completedSelectionOperation(ctx, tx, target)
	if err != nil {
		return ActivationManifest{}, err
	}
	if !found {
		return ActivationManifest{}, ErrRecordNotFound
	}
	var next int64
	if err := tx.QueryRowContext(ctx, `SELECT next_op_id FROM install_state WHERE install_id=?`, s.installID).Scan(&next); err != nil || id >= next {
		return ActivationManifest{}, ErrCorruptDatabase
	}
	m, _, err := s.loadActivationManifestConn(ctx, tx, id)
	if err != nil {
		return ActivationManifest{}, err
	}
	if m.TargetVersion != target.Version || m.StagedVersionDir != target.Directory || m.VerifiedArtifactSHA256 != target.ArtifactSHA256 {
		return ActivationManifest{}, ErrCorruptDatabase
	}
	if err := tx.Commit(); err != nil {
		return ActivationManifest{}, err
	}
	return m, nil
}
