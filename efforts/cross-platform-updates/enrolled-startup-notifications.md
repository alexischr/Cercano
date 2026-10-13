# Enrolled startup notification integration

Source a928181756c281c2db7ab6c7847736a49f9a2537.
Native gate https://github.com/cercano-ai/Cercano/actions/runs/38337770676
Windows/Linux/macOS pass, including the full CLI suite.

OpenExistingReadOnly requires an already-existing private managed directory chain
and current recognized database. It does not initialize databases/directories,
repair permissions, migrate schemas, or permit SQL writes. Recognized header
preflight refuses foreign/old/future main files before SQLite access; schemas and
installation identity are validated again on the read-only connection. A new
schema present only in uncheckpointed WAL is conservatively unavailable until
the installer checkpoints it. The reader uses mode=ro, not immutable=1, and tests
prove committed WAL records remain visible and writes remain refused even if
query_only is turned off.

Important precision: a disposable SQLite probe showed mode=ro may create/use
-wal and -shm coordination files. The guarantee is no database creation, migration
or application-data writes, NOT zero filesystem activity. This behavior is stated
in the API; SQLite coordination remains within the already protected updater
state directory. Unknown/absent ordinary installations are not created.

The existing agent startup now starts an optional in-process observer only when
an adjacent explicit enrollment record exists. No enrollment occurs on startup;
development and ordinary unenrolled/package-managed copies perform no updater DB
work. The observer reads already-current state, publishes only completed matching
installation facts through existing SubscribeEvents, and refuses silent identity
rebinding. It is canceled on agent cleanup; errors are deduplicated optional
path/content-free diagnostics, not readiness or version-connection gates. There
is no new service, listener or process.

Integration fixtures enroll an inert temporary launcher, verify missing state
stays missing, then create/complete a temporary installation while the observer
is running. The event is delivered and a replacement server reconstructs it.
No installed executable or live agent was used. Race/vet, command tests and the
full CLI suite also pass locally.

Remaining scope: versioned child executables require explicit installation context
from the stable bootstrap; this path does not search ancestors. Machine-scope and
package-managed identity integration, actual install/enrollment entrypoints,
force-stop/install/restart execution and full update UI remain unfinished. This
is not a completed end-to-end force update or production release rehearsal.
