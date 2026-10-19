# Native candidate verification — approved direction

The user approved updater-side native process/listener verification rather than adding a private credential/handshake. GetAgentHealth remains observational. Unknown process or endpoint ownership must never authorize health success. No new listener or credentials are authorized.

## Shared inspection foundation

The macOS libproc Inspect and HoldsListener implementations have been moved unchanged into internal/updatecoord/nativeprocess. brewrestart retains forwarding functions and an Identity type alias; its formula-specific ownership policy stays in brewrestart. This avoids making the generic updater depend on Homebrew layout policy and keeps a single native implementation.

A new disposable listener fixture checks current-process identity, owned loopback listening, stale lifetime rejection, foreign identity rejection and invalid endpoints. Existing Homebrew integration tests exercise the forwarding path.

Verification: both package race suites and vet pass locally on macOS. Builds pass with cgo disabled and for Windows/amd64 and Linux/amd64. Windows/Linux native inspection is still explicitly unsupported; those builds are not execution evidence. The existing updatecoord CI glob includes the new package, but no new CI run or push was performed.

## Remaining boundary

HoldsListener is a per-process observation, explicitly not proof of unique endpoint ownership. A responder's reported PID or a server-side endpoint self-check cannot replace updater-side evidence. Candidate launch lifetime binding, Windows/Linux inspection, competing listener handling, bounded health probing and reconnect evidence remain to be implemented before health may authorize activation completion. No live agent or installed files were changed.
