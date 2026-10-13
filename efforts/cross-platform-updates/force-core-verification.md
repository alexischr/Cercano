# Confirmed force-stop core — native verification

User approved interrupting proven agent-owned commands/workers for an explicit
force update. No unrelated process termination or automatic replay is authorized.
Source f9f8b686d6362125f42707388551469af688c35e.
Native run https://github.com/cercano-ai/Cercano/actions/runs/38466494724
Windows/Linux/macOS pass, including full CLI tests.

Internal RequestForceUpdateStop requires explicit confirmation, a stale-PID guard
(NOT authentication) and a configured process-local force owner. It seals new
admission immediately without erasing active counts, interrupts registered owned
contexts and invokes the owner once. Repeated requests remain idempotent.
Refusal/cancellation before commit leaves contexts/admission unchanged. There is
no self-signal or PID-kill fallback and no public force RPC.

Broker interruption preserves generations and replay so current interrupted turns
can still report terminal results, rather than being fenced out like superseded
profile-change turns. A reproduced late-registration race is fixed: turns arriving
after interruption get canceled context/never-current generation0 and cannot reset
old history. The process stop channel retains force intent across ordinary repeated
requests. Explicit force cleanup closes transports without starting GracefulStop,
then reuses owned-resource teardown. Transport stop is not handler/child/process
exit proof; the updater must still verify actual exit before any version switch.

Tests cover consent/identity/owner refusal, active lifetime preservation, context
interruption, late registration, replay/generation retention, concurrent repeated
commit, owned channel wakeup and a blocked owned RPC severed without waiting for
handler completion. Broker/server/command race suites and vet pass. Durable
interruption persistence, descendant termination and real updater invocation still
need end-to-end verification; these tests do not prove those unfinished aspects.

Pending authorization decision: inspected agent server uses recovery and admission
interceptors, and the client uses insecure transport credentials; no caller-auth
check was found for the proposed force surface. A boolean or PID is insufficient.
Recommend a protected installation-bound local update-record force request consumed
by the existing observer, with consent, exact process binding and freshness. The
alternative is a separately authenticated force request on the existing connection
with new credential lifecycle. No new authority or public endpoint has been added;
this security/destructive-control decision is awaiting approval.

No live agent, command or installed binary was stopped or changed.
