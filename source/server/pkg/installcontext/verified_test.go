package installcontext_test

// Regression coverage for the opaque verified context returned by the
// verification constructor. Both mains previously called
// VerifyStartup/VerifyChild and discarded the verified enrollment record;
// these tests pin the contract the consumers rely on: (a) the returned
// context carries exactly the enrollment record the verifier proved, (b) it
// derives the role-correct stable launcher location from the verified
// record only — never from raw locators or ancestor guessing — and (c) an
// absent (zero) context fails closed instead of minting an identity.
//
// This file was adapted from an earlier draft whose Verified type exposed
// the record and role as EXPORTED fields (and had VerifyChild return it).
// That shape was forgeable: any caller could mint a Verified identity from a
// fabricated enrollment.Record without any verification. The API is now
// opaque: unexported fields, accessors (Record/IsClient/StableLauncher), and
// a dedicated constructor (VerifyChildVerified) that wraps the unchanged
// VerifyChild and only ever returns a context the real verifier proved.
// VerifyChild/VerifyStartup keep returning enrollment.Record for existing
// callers. No schema, transport, or network involvement.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"cercano/source/server/pkg/installcontext"
)

func TestVerifiedContextExposesRoleCorrectStableLauncher(t *testing.T) {
	ctx := context.Background()
	f := verifiedChildFixture(t)

	agent, err := installcontext.VerifyChildVerified(ctx, f.locators, f.stateRoot, f.agentExe, false)
	if err != nil {
		t.Fatal(err)
	}
	if !agent.Present() {
		t.Fatal("verified agent context is absent")
	}
	if agent.Record() != f.record {
		t.Fatalf("agent context record = %+v, want the verified enrollment record %+v", agent.Record(), f.record)
	}
	if agent.IsClient() {
		t.Fatal("agent role context must not claim the client role")
	}
	want := filepath.Join(f.record.Root, f.record.Agent)
	if got, e := agent.StableLauncher(); e != nil || got != want {
		t.Fatalf("agent stable launcher = %q err %v, want %q derived from the verified record", got, e, want)
	}

	client, err := installcontext.VerifyChildVerified(ctx, f.locators, f.stateRoot, f.clientExe, true)
	if err != nil {
		t.Fatal(err)
	}
	if client.Record() != f.record || !client.IsClient() {
		t.Fatalf("client context = %+v client=%t, want the verified enrollment record with client role", client.Record(), client.IsClient())
	}
	want = filepath.Join(f.record.Root, f.record.Client)
	if got, e := client.StableLauncher(); e != nil || got != want {
		t.Fatalf("client stable launcher = %q err %v, want %q derived from the verified record", got, e, want)
	}
}

func TestAbsentVerifiedContextFailsClosed(t *testing.T) {
	var zero installcontext.Verified
	if zero.Present() {
		t.Fatal("zero verified context claims to be present")
	}
	if _, err := zero.StableLauncher(); err == nil {
		t.Fatal("absent verified context minted a stable launcher; it must fail closed")
	}
}

func TestVerifiedContextRefusesUnverifiedExecutable(t *testing.T) {
	// An unverified running image yields an error, never a usable context:
	// the value and error stay paired (the returned context is absent) so a
	// caller cannot accidentally use a half-verified identity.
	f := verifiedChildFixture(t)
	v, err := installcontext.VerifyChildVerified(context.Background(), f.locators, os.TempDir(), filepath.Join(os.TempDir(), "cercano"), false)
	if err == nil {
		t.Fatal("unverified executable accepted")
	}
	if v.Present() {
		t.Fatal("failed verification returned a present context")
	}
	if _, err = v.StableLauncher(); err == nil {
		t.Fatal("failed-verification context minted a stable launcher")
	}
}
