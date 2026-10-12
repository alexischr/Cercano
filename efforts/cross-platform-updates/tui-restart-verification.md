# Per-terminal TUI restart — implemented command and handoff

Approved behavior: /restart is TUI-only, uses y/n/d confirmation, and does not
restart the shared agent or other terminals. Windows restart is manual initially.

Source 8c4cb8fac084 adds the command, UI prompt and Unix handoff. Native run:
https://github.com/cercano-ai/Cercano/actions/runs/38294961915
HEAD5f9eb6db5206: Windows/Linux/macOS passed, including the new targeted CLI gate.

Unix uses Bubble Tea's terminal-release wrapper followed by exec replacement.
Conversation ID, working directory and unsent text travel through a bounded,
private, owned, unlinked inherited descriptor. Drafts are not placed in argv,
environment or a lasting disk file. Decode refuses missing/null/duplicate/unknown
fields, invalid/oversized data and nonprivate/named descriptors. Capability
probing uses the old client's known offline --version path, never an unknown
startup subcommand. Unsupported replacement clients refuse with manual guidance.

The receiving TUI resumes the conversation, schedules its history hydration during
Init and restores the draft without submitting it. Failures before exec retain
the existing client; resume failures in the new client preserve the draft and
identify the conversation for /history. Active terminal work, pending decisions,
auth flows and open content views refuse restart rather than discard state. Other
transient views are not serialized. Windows confirmation shows save-draft,
/quit/reopen and /history instructions instead of spawning another TUI.

Tests include a real replacement of an owned test process, strict handoff parsing,
legacy capability refusal without starting the old client, confirmation/details/
deferral, preservation of draft and pending decisions, hydration initialization,
and Windows manual behavior. Focused race tests, whole CLI compilation, vet,
actual binary capability output and Windows cross-compilation also passed locally.
A full CLI test run still fails TestRoutingNavigationCancellationAndEighthTab;
that routing test was not changed and the full CLI suite is not claimed green.

The first native run failed before Windows CLI tests in an existing persistence
fixture: buffered hydration error delivery was incorrectly assumed to imply the
worker's deferred release had run. The test now waits boundedly for the observed
release and still asserts exactly once;100 race repetitions pass. Production
persistence behavior was not changed.

Remaining: TUIUpdateAppliedMsg is a trusted integration seam, not yet an updater
broadcast. Until versioned-install resolution is connected, manual /restart uses
the current executable location; it cannot discover a different version directory
by guessing. The updater must provide its verified selected client path and send
the installed-update notification. No open user terminal or live agent was
restarted, and the complete force-update/reconnect flow is not finished.
