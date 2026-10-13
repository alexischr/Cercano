# Protected local force authorization — implementation and native verification

User approved this local-record authorization approach instead of exposing a new
unauthenticated destructive network endpoint. Source48d42b1bfa373cede3fd6ccaf295f8996f0e6b65.
Native run https://github.com/cercano-ai/Cercano/actions/runs/38480521433
Windows/Linux/macOS pass, including the full CLI suite.

Schema4 adds typed force_agent_sessions and force_stop_intents tables. Exact
schemas1–3 are preserved and explicitly opened legacy databases migrate additively
in one transaction. Tests preserve legacy journals/records and roll back injected
migration failure. Optional existing-state control/notification openers do not
initialize directories/databases or migrate. Five stale current-version fixture
expectations were updated; legacy definitions were not rewritten.

An owned Server lifetime creates one cryptographically random boot identity kept
in memory, with its actual PID. It is public identity, not a password. Authorization
comes from the protected installation-bound local state. The existing in-agent
observer publishes that identity once into already-current updater state and may
consume local force requests. Observer restarts do not mint another identity or
silently rebind installations. Registration failure is reconciled by readback,
not repeated overwrites of another agent's session.

Submitting a request requires separate explicit force consent, the exact current
agent session, a current consented draining operation and matching prepared
activation journal. Requests are immutable per operation, valid for at most30s,
and rechecked against a clock inside the write transaction. Consumption checks
the same facts, rejects expiry/replacement/replay/corruption, and is committed
atomically before invoking the process-local stop core with the remaining deadline.
A crash after consumption never replays force. Consumption and callback acceptance
are not process-exit, install-success or health evidence.

ForceControl exposes only session/request operations over existing current state.
Unix state files require the current uid, private mode, regular-file identity and
single-link checks. Windows uses actual owner/DACL checks; a SQLite file may inherit
trusted ACEs only under a separately verified protected parent. Unknown/world grants
refuse without repair. Existing strict directory/identity-record policy stays
strict. Native Windows tests cover trusted inheritance and world-access refusal.
SQLite still uses its normal WAL/SHM coordination files as documented previously.

Verification includes state race tests, exact migration/refusal, cancellation and
expiry rollback, corruption/type checks, two-handle exactly-once consumption and
reopen replay refusal, replacement-agent/same-PID refusal, fixed server lifetime
identity, installation-rebind refusal and actual observer-loop consumption leading
to a single fixture callback. State/permission/server vet, command tests and full
CLI tests pass locally. No force network RPC, listener, shared credential or new
service was added; no live agent/command was stopped.

Still unfinished: updater/UI request-production wiring, independent process/owned
child exit verification, durable interrupted-tool recovery, version selection and
restart/reconnect composition, and package-manager backends. These fixtures do not
claim a completed real force upgrade or safe automatic replay/rollback of external
side effects.
