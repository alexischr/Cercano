# Stable launcher handoff — local and native CI verification

The user approved implementing the full stable launcher with temporary-installation tests only. The entrypoint integration is now in both agent and CLI mains after private installation-context verification and before normal command/config initialization. Private updater dispatch retains its existing earlier position.

## Implemented boundary

- Only an adjacent explicit enrollment record enables dispatch. No PATH or ancestor discovery. Missing enrollment leaves existing startup unchanged; malformed existing enrollment refuses startup.
- Open existing protected updater state without initialization/migration. Acquire shared launch exclusion before observing selection/manifest; retain it through process creation/replacement.
- Resolve a unique role member from the exact completed selection's manifest, or the previously approved selected candidate gate. Independently verify image length/hash, enrollment and selection readback. A candidate is not thereby healthy or complete.
- Prepend only the two approved installation locators to ordinary argv, preserving `--` and all positional arguments. The selected child performs existing independent startup verification; its opaque verified context prevents recursive dispatch.
- Unix uses exec, retaining terminal streams, cwd, PID, signal behavior and explicitly inherited TUI restart descriptors. The exclusion file opened by Go is close-on-exec: the shared lease closes on successful replacement, or the deferred close runs on failure.
- Windows starts a foreground child sharing its console/standard streams and job context, releases launch exclusion immediately after Start, then waits and returns its exit code. No detached updater flags or job breakaway are used. The wrapper does not send duplicate console interrupts.

## Verification performed locally

- macOS race tests for stablelaunch, installcontext and installcontexttest pass.
- Real temporary selected test executables prove both roles, both completed and candidate startup, argv/stdin/stdout/stderr/cwd/exit status, Unix PID preservation and post-handoff exclusive-lock acquisition while the child is alive.
- Unix inherited-descriptor fixture passes (covers the descriptor mechanism used by TUI restart).
- Refusals cover modified images, wrong role, malformed/missing selection, malformed enrollment, nested locator dispatch and exclusive update contention. Unenrolled startup does not initialize a missing state base.
- Real agent and CLI binaries built locally and invoked through temporary enrolled stable paths pass their offline version commands without creating normal application configuration or starting an agent/TUI.
- Agent command suite, full CLI suite, and focused vet pass.
- Windows amd64 launcher fixture and agent compile; Windows vet passes. Linux amd64 launcher fixture compiles. Windows/Linux CLI binaries compile.
- Following explicit push authorization, native CI run [39163845140](https://github.com/cercano-ai/Cercano/actions/runs/39163845140) passed on Windows, Linux and macOS at `2acbd98108383a91a040fb478a4de70192ca01e2`, including the full CLI suite.
- Earlier runs exposed fixture-only portability defects: shared installation fixtures created permissive directories before private-directory validation, and source-wiring tests retained CRLF carriage returns. Commits `8795fc013389` and `2acbd9810838` fix those fixtures without relaxing production checks. The CRLF failure was reproduced before fixing.

## Limits

Native Windows/Linux fixture execution passed; real interactive console acceptance remains pending. This is launch admission, not proof that processes and descendants have exited or that health succeeded. Full updater invocation, owned-process lifecycle, recovery/cleanup and package-manager integration are still separate plan work. No live enrollment, installed selection, database migration, agent restart or publication was performed. The pre-existing locators_test.go edit is excluded from this checkpoint.
