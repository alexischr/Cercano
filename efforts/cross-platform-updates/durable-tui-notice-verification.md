# Durable TUI notice source — native verification and ownership boundary

Native run https://github.com/cercano-ai/Cercano/actions/runs/38308420694
Source 339fb64a2648092218c3057908e0ad7b8f605e3c. Windows/Linux/macOS pass,
including full CLI and notification transport tests.

CompletedSelectionOperation reads existing SQLite records in a read transaction.
It requires one matching journal with the exact version, relative directory,
artifact digest and selection generation, a complete checkpoint, and a valid
matching completed/health-verified operation without pending recovery. Historical
completed operations remain discoverable when a newer check is current. Unknown,
incomplete, corrupt, foreign or ambiguous state refuses notification; no records,
counters, schemas or directories are created or modified.

SyncTUIUpdateFromState validates the supplied store's protected directory and
installation association, reads/rechecks the selection around that query, and
feeds the existing metadata-only event hook. Tests reopen the database and build
a replacement Server to reconstruct the same notice without rewriting selection.
State/server race tests and vet also pass locally. This is a validated bridge,
not yet an automatic startup lookup or complete updater installation flow.

Startup still lacks authoritative installation discovery. installation's current
value model consumes caller-provided ownership evidence and explicitly forbids
layout guessing and silent development/archive enrollment. Automatically choosing
an ID/root or opening/migrating an arbitrary updater database would bypass that
contract. Normal application use must remain independent of notification failure.

Pending decision: recommend an installer-owned, versioned identity record beside
the stable launcher for explicitly enrolled self-managed installs. Alternative:
a protected per-user installation registry with stale-entry/move reconciliation.
Package-managed installs still require package-manager ownership corroboration.
This defines persisted enrollment/write authority and is awaiting user approval
before creation/startup wiring. It does not authorize enrolling the developer's
installation, modifying package-managed files directly, or changing live data.
