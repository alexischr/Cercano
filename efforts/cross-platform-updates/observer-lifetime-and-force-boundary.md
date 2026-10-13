# Bootstrap lifetime correction and force-update boundary

Native run https://github.com/cercano-ai/Cercano/actions/runs/38340046130
Source50a16a7008fde9c03008b8a66f852288fb271659; Windows/Linux/macOS pass.

Tracing the executor exposed a bootstrap integration error not caught by the
previous native gate: startGRPCServer returns a cleanup function while the agent
continues running. Deferring stopTUINotices in that startup function canceled the
observer at return. A regression against the actual bootstrap AST failed before
the fix. The defer is removed; cancellation remains in returned agent cleanup.
The guard now runs natively. It verifies ownership placement without launching
an agent with real startup side effects; owned server fixtures separately verify
the observer loop and completion/replay behavior. Earlier green component tests
must not be read as proof that this previously incorrect wiring worked.

Current force-upgrade blocker: the executor requires its owned work to quiesce,
and normal shutdown still drains before stopping providers/workers. The user
requested an immediate force update, but active child-command treatment is a
new explicit destructive/user-data decision. Recommend stopping only proven
agent-owned commands/workers after a clear all-terminal force confirmation,
marking work interrupted and never automatically replaying it. Alternative:
refuse while commands are active and require manual cancellation before retry.
Unrelated/unproven processes are never candidates for signaling in either case.
The choice is awaiting approval; no forced child termination or live update was
implemented. This does not reopen the approved TUI restart/enrollment decisions.
