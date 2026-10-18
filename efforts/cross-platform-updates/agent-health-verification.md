# Read-only agent health RPC verification

Approved interface: dedicated additive `GetAgentHealth` on the existing Agent service, not lifecycle fields in GetConfig. Implemented in local checkpoint `8116245a5237`.

The response reports build version, observational PID, readiness and stable not-ready reasons: `startup_incomplete`, `admission_paused`, `shutdown_begun`. Startup readiness is explicitly marked by the entrypoint after listener/service wiring and launching Serve. Shutdown marks readiness false before closing subscriptions. The health method is exempt from update-work admission and does not acquire or release work. It is an instantaneous observation, not a reservation or a guarantee of subsequent availability.

The client wrapper propagates errors, including old-server Unimplemented. Missing evidence must not be treated as health success. PID is not authentication or installation ownership evidence. There is no new listener, destructive control method, or ordinary client version-compatibility gate.

Verification:
- Implementation task reports full internal/server and pkg/agentclient race tests, focused vet, and server/CLI builds passed.
- Independent read-only review found no blockers and ran all eight server health tests successfully.
- Main session independently ran `go test -race -count=1 -timeout=120s -run 'TestGetAgentHealth|TestHealth' ./internal/server ./pkg/agentclient`: both packages passed.
- Tests include initial not-ready, ready, admission pause, shutdown, combined reasons, wire availability while paused, client mapping/error handling, and Unimplemented behavior via an unimplemented service fixture.

Limitations: no new native CI run, real old-binary test, live process action or push was performed for this slice. Full update health success still requires updater-side ownership/selected-image binding, expected-version comparison, bounded probing and client reconnect evidence. This RPC alone does not complete activation, rollback or the overall update effort. The unrelated locators_test.go edit was preserved.
