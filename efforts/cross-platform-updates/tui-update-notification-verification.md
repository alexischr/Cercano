# Installed TUI notifications — transport and UI verification

Native run: https://github.com/cercano-ai/Cercano/actions/runs/38301139416
Source 7f973c719938cbcdacbaef9a74d9703e94106648. Windows/Linux/macOS pass,
including the full CLI suite, not merely client compilation or restart fixtures.

An additive SubscribeEvents message carries only installed version and operation
ID. It is not an executable path, remote restart instruction or new RPC/service.
NotifyTUIUpdateInstalled is a trusted installer hook with input validation and
operation deduplication/conflict refusal. A separate coalesced state signal avoids
dropping other event types and replays the latest notice on new subscriptions to
the same server. Real gRPC tests cover two subscribers, later subscriptions,
updates, duplicates and conflicts; SDK tests preserve other/unknown event handling.

TUIs confirm independently. Deferring in one does not affect another; busy or
pending-decision clients preserve their state and offer /restart later. Replayed
notices do not reopen a dismissed prompt, and a client already running that
version ignores it. A notified Unix restart checks the locally resolved target's
version before exec in addition to handoff support, refusing stale targets with
manual guidance. No version mismatch blocks ordinary agent/client communication.
Windows restart remains manual.

Full-suite verification corrected a stale tab8 fixture (Token Metrics is8,
Context9) while retaining dirty-routing protection. Windows coverage exposed
backslash stripping in image-path paste, fixed with drive/UNC preservation; path
presentation now normalizes separators after native workspace/home handling.
Tests use native paths and golden files have fixed LF checkout endings. Trace
redaction checks still run everywhere; Unix mode bits are not asserted as Windows
ACL proof. No trace permissions were rewritten or broader ACL guarantee claimed.
Restart now refuses attachments/queued turns instead of losing state not included
in the text-only handoff. Regressions reproduced both image-path corruption and
attachment-loss risk before those fixes. CI triggers now include CLI-only edits.

Still pending: actual install completion must invoke the hook, and replacement
agent startup must initialize its snapshot from authoritative installation state.
Notifications alone do not resolve a versioned executable location; that remains
the local install controller's responsibility. The full force-upgrade/reconnect
flow is not finished. No live terminal/agent was restarted, no current installation
was upgraded, and no production release or main merge occurred.
