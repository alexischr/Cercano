package activationtxn

import (
	"context"
	"errors"
	"reflect"
	"sort"

	"cercano/source/server/internal/updatecoord/state"
)

// verifyAndBindTargetImages persists authenticated, freshly checked executable
// facts before switch intent. After intent, missing legacy facts fail closed:
// recovery must not invent fingerprints from the current filesystem.
func verifyAndBindTargetImages(ctx context.Context, req Request, journal state.ActivationJournal) error {
	if err := verifyTargetImages(ctx, req, journal); err != nil {
		return err
	}
	p := req.Images
	wanted := state.ActivationManifest{SchemaVersion: state.ActivationManifestSchemaVersion, InstallID: journal.InstallID, OpID: journal.OpID, TargetVersion: journal.TargetVersion, StagedVersionDir: journal.StagedVersionDir, VerifiedArtifactSHA256: journal.VerifiedArtifactSHA256}
	for _, member := range p.Manifest.Members {
		if member.Name == p.AgentMember || member.Name == p.ClientMember {
			wanted.Members = append(wanted.Members, state.ManifestMember{Path: member.Name, Length: member.Size, SHA256: member.SHA256})
		}
	}
	sort.Slice(wanted.Members, func(i, j int) bool { return wanted.Members[i].Path < wanted.Members[j].Path })
	if len(wanted.Members) != 2 {
		return ErrTargetImages
	}
	if err := state.ValidateActivationManifest(wanted, journal.InstallID); err != nil {
		return errors.Join(ErrTargetImages, err)
	}
	stored, _, err := req.Store.LoadActivationManifest(ctx, journal.OpID)
	if errors.Is(err, state.ErrRecordNotFound) && journal.Checkpoint == state.JournalPrepared {
		if _, err = req.Store.SaveActivationManifest(ctx, wanted); err != nil {
			return errors.Join(ErrTargetImages, err)
		}
		return nil
	}
	if err != nil {
		return errors.Join(ErrTargetImages, err)
	}
	sort.Slice(stored.Members, func(i, j int) bool { return stored.Members[i].Path < stored.Members[j].Path })
	if !reflect.DeepEqual(stored, wanted) {
		return ErrTargetImages
	}
	return nil
}
