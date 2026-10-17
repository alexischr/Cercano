package installcontext

import (
	"context"
	"errors"
	"path/filepath"

	"cercano/source/server/internal/updatecoord/enrollment"
)

// ErrAbsentVerified reports an operation on the zero Verified context. The
// zero value is absent and every operation on it fails closed; only a
// successful verification constructor produces a present Verified.
var ErrAbsentVerified = errors.New("installcontext: absent verified installation context")

// Verified is the unforgeable result of a successful completed-installation
// verification: it carries exactly the enrollment record the internal
// verifier proved, bound to the launcher role that was verified. Its fields
// are UNEXPORTED, so no caller can fabricate a Verified from a forged
// enrollment.Record: the only producing paths are the verification
// constructors below, which always perform the real verification (they wrap
// VerifyChild unchanged and never accept a caller-supplied record).
//
// The zero value is absent: Present is false and every accessor fails
// closed. Passing it onward (e.g. to forcesave.DiscoverVerified) is refused.
type Verified struct {
	record enrollment.Record
	client bool
}

// VerifyChildVerified performs the same completed-installation verification
// as VerifyChild — it wraps VerifyChild unchanged, including its trusted
// stateRoot/executable inputs and role argument — and returns the opaque
// Verified context carrying the proven record and role. Callers that only
// need the record keep using VerifyChild; callers that must pass the proven
// identity onward use this constructor so a caller-fabricated record can
// never be smuggled into a consumer API. On any verification failure the
// returned Verified is absent (zero) and paired with the error, so a caller
// cannot accidentally use a half-verified identity.
func VerifyChildVerified(ctx context.Context, locators Locators, stateRoot, executable string, client bool) (Verified, error) {
	record, err := VerifyChild(ctx, locators, stateRoot, executable, client)
	if err != nil {
		return Verified{}, err
	}
	return Verified{record: record, client: client}, nil
}

// Present reports whether this context was produced by a successful
// verification. The zero value is absent.
func (v Verified) Present() bool { return v.record.InstallID != "" }

// Record returns the enrollment record the verifier proved. It is an
// accessor only: the value cannot be turned back into a Verified by any
// other package.
func (v Verified) Record() enrollment.Record { return v.record }

// IsClient reports the fixed launcher role that was verified: true is the
// cercano-cli client role, false the cercano agent role.
func (v Verified) IsClient() bool { return v.client }

// StableLauncher returns the role-correct stable launcher location derived
// ONLY from the verified record (never from raw locators, argv or ancestor
// guessing): <verified root>/<role member name>. An absent context refuses.
func (v Verified) StableLauncher() (string, error) {
	if !v.Present() {
		return "", ErrAbsentVerified
	}
	name := v.record.Agent
	if v.client {
		name = v.record.Client
	}
	return filepath.Join(v.record.Root, name), nil
}
