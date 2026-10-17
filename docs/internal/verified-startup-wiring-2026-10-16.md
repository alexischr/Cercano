# Verified startup-context wiring — verification record (2026-10-16)

Bounded slice: mains/observer wiring of the committed opaque
`installcontext.Verified` context (e1794403). No migration, no new RPC, no
changed completed-only policy, no raw record→Verified minting anywhere.

## What changed

- `source/server/pkg/installcontext/verify.go` — new `VerifyStartupVerified`
  returning the opaque `Verified` context; the trusted-input derivation
  (os.Executable + DefaultStateRoot) happens exactly once and delegates to the
  real verification in `VerifyChildVerified`. `VerifyStartup` keeps returning
  the bare `enrollment.Record` for existing callers by extracting it from the
  verified result.
- Mains (`source/server/cmd/cercano/main.go`, `source/clients/cli/main.go`) —
  `claimInstallContext` now returns `(Verified, code, ok)`; the early parse
  ordering is intact (first gate before any flag parsing), refusals still
  `os.Exit(code)` with the same codes. Other modes are preserved: the verified
  context is only used by the TUI-notices observer path on the server and the
  CLI model.
- Agent `runServerMode`/`startGRPCServer` thread the verified context through;
  `startInstalledTUINotices` resolves the stable launcher **from the verified
  record** (never this process's own executable) and requires an exact
  enrollment match before binding the existing watch; a stale startup record
  refuses rather than rebinding to the new on-disk identity. Once bound, the
  existing `WatchEnrolledTUIUpdates` revalidation governs. Without a verified
  context the pre-verification behavior is unchanged.
- CLI `Model.WithVerifiedContext` stores the verified context;
  `discoverForceSaveCmd` routes a present verified context to
  `forcesave.DiscoverVerified` (exact-record-match refusal on staleness) and
  an absent context to the unchanged `forcesave.Discover`.
- No process-global mutable context; propagation is by parameter/field only.

## Verification performed (commands run)

- `cd source/server && go test -race ./cmd/cercano/ ./pkg/installcontext/ ./pkg/forcesave/ ./internal/updatecoord/...` — all ok.
- `cd source/server && go vet ./...` — clean.
- `cd source/clients/cli && go test -race -count=1 ./...` — all ok (full CLI).
- `cd source/clients/cli && go vet ./...` — clean.
- Cross-compilation: `GOOS=windows GOARCH=amd64 go build ./...` and
  `GOOS=linux GOARCH=amd64 go build ./...` for **both** modules — clean.

## New tests

- `source/clients/cli/internal/ui/force_save_routing_test.go` — verified
  client routes through the verified stable launcher; stale startup record
  refuses rebind (`forcesave.ErrUnavailable`); zero verified context keeps
  unchanged no-context behavior; `Model.WithVerifiedContext` stores and
  propagates exactly the verified value; absent-context startup command yields
  no client.
- `source/server/cmd/cercano/tui_update_notices_verified_test.go` — verified
  agent binds the observer to the verified stable launcher; stale startup
  record refuses rebind (no binding, no diagnostics); absent verified context
  leaves the no-context path unchanged (no diagnostics, no server touched).
- Updated mains wiring tests (`main_wiring_test.go` both mains) assert the
  verified propagation through `startGRPCServer` and the observer call, with
  the early parse ordering intact; `child_context_test.go` (both mains) assert
  the `(Verified, code, ok)` claim shape, including verify-before-init
  refusal cases that never reach config/DB initialization.

## Constraints honored

- Verified can only be minted by `VerifyChildVerified`/`VerifyStartupVerified`
  (opaque, unexported fields; zero value fails closed via `ErrAbsentVerified`).
- Single verification pass — the convenience `VerifyStartupVerified` shares
  the state-root/executable derivation with `VerifyStartup`; no double hash.
- No live agent, no live state, no push: all tests build throwaway enrolled
  fixtures via `installcontexttest` in temp dirs.
