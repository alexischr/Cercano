# Native candidate verification — approved direction

The user approved updater-side native process/listener verification rather than adding a private credential/handshake. GetAgentHealth remains observational. Unknown process or endpoint ownership must never authorize health success. No new listener or credentials are authorized.

## Shared inspection foundation

The macOS libproc Inspect and HoldsListener implementations have been moved unchanged into internal/updatecoord/nativeprocess. brewrestart retains forwarding functions and an Identity type alias; its formula-specific ownership policy stays in brewrestart. This avoids making the generic updater depend on Homebrew layout policy and keeps a single native implementation.

A new disposable listener fixture checks current-process identity, owned loopback listening, stale lifetime rejection, foreign identity rejection and invalid endpoints. Existing Homebrew integration tests exercise the forwarding path.

Verification: both package race suites and vet pass locally on macOS. Builds pass with cgo disabled and for Windows/amd64 and Linux/amd64. Windows/Linux native inspection is still explicitly unsupported; those builds are not execution evidence. The existing updatecoord CI glob includes the new package, but no new CI run or push was performed.

## Linux identity slice

Linux Inspect now reads bounded stat/status records and the kernel executable link through an anchored /proc directory, repeating identity observations before returning. Linux start ticks have their own Identity field, distinct from macOS wall-clock start seconds/microseconds. Missing proc data remains uncertainty, never manufactured ESRCH proof. Listener inspection still returns an explicit unsupported error.

Portable parser/lifetime tests and existing macOS race suites pass. Linux vet and test compilation, plus Windows builds, pass. Following the user's push authorization, native CI run 39187460069 at 7e867a5d8e50561dd81e51ad7af4973376ec1bdd completed successfully on ubuntu-24.04, windows-latest and macos-15. The Linux-only identity fixtures now have native execution evidence; they check the actual current process and invalid/absent PIDs. This clears the identity-slice verification blocker, not the still-unsupported Linux listener check or end-to-end health gate.

Run: https://github.com/cercano-ai/Cercano/actions/runs/39187460069

## Remaining boundary

HoldsListener is a per-process observation, explicitly not proof of unique endpoint ownership. A responder's reported PID or a server-side endpoint self-check cannot replace updater-side evidence. Candidate launch lifetime binding, Windows/Linux inspection, competing listener handling, bounded health probing and reconnect evidence remain to be implemented before health may authorize activation completion. No live agent or installed files were changed.

## Linux listener slice — approved scope and implementation

User approval (this session, no later instruction supersedes it): bounded Linux HoldsListener in internal/updatecoord/nativeprocess, using /proc TCP tables matched to exact process socket descriptors, repeated identity checks, no elevation, and fail-closed uncertainty. Scope guardrails honored:

- Target-network-namespace tables (/proc/<pid>/net/tcp{,6}) via an anchored /proc/<pid> root, not the host namespace tables.
- Endpoint restricted to explicit loopback address, nonzero port, no zone. Stale/foreign identity and any user other than the current UID are rejected; identity is re-inspected after the socket observation.
- Exact family/address matches plus family-appropriate wildcard bindings are accepted. A dual-stack IPv6 wildcard socket asked about an IPv4 endpoint is explicitly undecided — /proc does not expose IPV6_V6ONLY — and is reported as uncertainty, not as a match or a miss. Never claims exclusive endpoint ownership or service health.
- Two socket-inode sets bracket the table reads through anchored /proc/<pid>/fd; a changed set, vanished descriptor during enumeration, unreadable input or invalid parsed framing/fields is an error. These repeated observations are not an atomic kernel snapshot and cannot exclude changes that occur and reverse between samples.
- All reads bounded (fd count, read sizes, table bytes, batched directory reads); kernel word endianness handled via native layout; the decimal inode column and hex address/port/state columns are decoded accordingly.
- Pure parsers (listener_parse.go) are platform-independent and covered by portable tests: table parsing for IPv4/IPv6, wildcard matching, dual-stack ambiguity, malformed/truncated/oversized record rejection, socket-link decoding and bounded-read rejection.

Linux-native fixtures (listener_linux_test.go): owned IPv4 listener, IPv6 loopback and wildcard, IPv4 wildcard, closed listener becomes verified false only after fd stability, stale-identity and foreign-UID rejection, invalid endpoints (non-loopback, zero port, zoned), a helper process proving an unrelated same-user process's listener is not ours and is ours when asked for its own identity, vanished-process uncertainty instead of clean absence, and a churn test asserting fd churn can never produce a verified false for a held listener.

Verification honesty: macOS host — go test, go vet and go test -race for updatecoord and brewrestart all pass, full module builds. Linux/amd64 with cgo disabled — go build ./..., go vet and test compilation pass. No Linux runtime (no docker/podman/lima/colima on this host) and no CI run: the Linux-only fixtures have no native execution evidence yet. Windows remains unsupported. Darwin implementation is untouched. No production health wiring or live agent actions. Native Linux verification remains pending; local compilation is not a substitute.

Review caught legitimate non-LISTEN TIME_WAIT rows with zero inode being rejected; a failing portable regression demonstrated the issue before correction. LISTEN rows still require nonzero inodes, including off-port rows. Test subprocesses are bounded and reaped on failure paths.

Support limits: both TCP tables must be readable. A host with IPv6 disabled and no tcp6 table returns uncertainty even for IPv4; missing data is not interpreted as absence. The observed listener is in the target's network namespace, which does not establish reachability from the updater's namespace. No exclusivity, exit, or health-success inference is authorized.
