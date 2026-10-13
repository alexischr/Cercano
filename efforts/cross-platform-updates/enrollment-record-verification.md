# Installer-owned identity — per-user implementation

Approved by the user: create a small installer-owned ID record beside explicitly
installed/enrolled self-managed launchers, never silently enroll development
checkouts. Source b1a11bad9a1c76b308cb70287ac451065f74baf6.
Native run https://github.com/cercano-ai/Cercano/actions/runs/38329398055
Windows/Linux/macOS pass, including full CLI tests.

The .cercano-install.json format records schema version1, a random128-bit ID,
exact canonical root, per-user scope, host OS and fixed agent/client launcher
names. It contains no credentials, arbitrary executable commands or updater-state
paths. Strict bounded decoding refuses missing/null/duplicate/unknown fields,
future versions and copied records bound to another root.

Explicit Create requires an existing protected root and regular launcher files;
it refuses .git ancestors, symlinked roots and existing/corrupt records unchanged.
Same-directory no-replace publication yields one winner under concurrent creators.
Read never creates, migrates or repairs. DiscoverStableLauncher reads only the
adjacent record at a supplied stable-launcher path; it does not search ancestors
or bind arbitrary versioned/development executables to another installation.
Package ownership and updater execution authority still require independent
corroboration; the record alone cannot override managed ownership or elevate.

File access is checked on opened handles: Unix uid/mode/link checks; Windows real
token, trusted owner and protected DACL checks. New files receive private access;
unsafe existing permissions are refused rather than repaired. Windows native
fixtures verify refusal of a world-accessible file without changing its DACL.
Windows publication explicitly reports directory-entry durability uncertainty;
callers must inspect the published record rather than overwrite/re-enroll. No
physical power-loss guarantee is claimed.

Race/vet fixtures cover explicit creation, no creation on read, stability across
launcher changes, concurrent creators, corrupt-data preservation, strict format,
root binding, development trees, links, cancellation and bounded discovery.
No actual checkout or installation was enrolled or modified.

Next: provide a strictly existing-state notification reader before startup uses
this ID. The current state.Open can create directories and migrate schemas, so
it is deliberately not wired into ordinary optional notification discovery.
Bootstrap/installer entrypoints and machine/package-managed identity are still
pending; this is not the completed force-upgrade/reconnect flow.
