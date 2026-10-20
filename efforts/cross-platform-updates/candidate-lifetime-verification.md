# Owned candidate lifetime capture

Implements the candidate-wrapper scope approved in commit `4b0c13bb65f6`.

`candidate.Start` uses `launch.LaunchObserved` to capture and repeat native identity observations before the sole asynchronous reaper starts. It compares the observed executable's file identity with the explicitly supplied executable; it never searches PATH or adopts an existing PID. Generic `launch.Launch` retains its existing behavior.

Lifetime boundary:

- Linux uses Go 1.26 `os.Process.WithHandle` and polls the owned pidfd before and after inspection. Missing pidfd support is refusal, not a PID-only fallback.
- Windows uses `WithHandle` and zero-time waits on the owned process handle before and after inspection.
- macOS with cgo uses non-reaping `waitid(WNOWAIT|WNOHANG|WEXITED)` checks before and after inspection. Exited/non-child statuses refuse, as do SIGCHLD dispositions that disable zombie retention. The launcher remains the sole reaper; unrelated code must not reap its children or change process-wide SIGCHLD policy during capture.
- Unsupported observation configurations refuse identity capture without changing generic helper launch support.

Post-start inspection failure (including an observer panic) returns the owned process alongside an error and still starts its reaper. The caller retains completion access but gets no candidate identity. It must not blindly retry a launch on such an error. `Process.Done` is only a reaper-return notification; the existing `WaitForCompletion` receipt remains necessary to distinguish confirmed child termination from an uncertain wait failure.

`Candidate.Identity` refuses failed captures and completed launches and compares fresh native observations with the captured lifetime. It is a point-in-time observation, not a guarantee the process remains alive after return. No health-success, endpoint exclusivity, signaling, selection mutation, descendant completion, or installation authority is added. There is no production updater integration in this slice.

Verification on the macOS development host:

- Full `go test -race ./internal/updatecoord/... -count=1 -timeout=150s` passes.
- `go vet ./internal/updatecoord/...` passes.
- No-cgo updater build passes.
- Windows/amd64 and Linux/amd64 launch/candidate builds and vet pass; candidate test binaries compile for both, and launch tests compile for Windows.
- Windows candidate tests reexecute inside a fixture-owned permissive job and run the same identity/failure/completion assertions. They do not modify the inherited job or relax production breakaway flags, and positive cases do not silently skip.
- Command and Homebrew restart package tests pass. Focused observation race tests also pass three repeated runs.
- Fixtures cover live, exited, reaped and failed observations, observer panic, wrong PID/image/lifetime, completion after failed capture, and refusal after completion. All processes and paths are disposable fixtures; no live agent or installation was changed.

Native Windows/Linux execution is still required for this new slice. Cross-compilation is not runtime verification. The existing native updater workflow includes these packages via `./internal/updatecoord/...`; this work has not been pushed or run remotely.
