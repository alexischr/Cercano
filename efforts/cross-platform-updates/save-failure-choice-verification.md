# Cancel / Force anyway after interruption-save failure

Approved behavior: a failed interruption-history write leaves active work running.
A separate explicit Force anyway choice may proceed with incomplete history.
This does not change ordinary restart or TUI restart behavior.

Implementation checkpoints: server gate 8cdd8fc368ff; additive protected decision
store 243e7695b369; TUI prompt dc45c2a00b72; native gate 4be785c4926a;
Windows fixture correction c679155db417. The server polling bridge retains an
opaque process-local receipt, publishes a fresh receipt identity, reconciles
ambiguous publication without resetting decisions, and consumes each authorized
decision once. Cleanup revokes retained receipts and prevents resurrection.

Native run https://github.com/cercano-ai/Cercano/actions/runs/38645757171
passed Windows, Linux and macOS at c679155db417c04d0609d33f670891876dd291b5.
Its predecessor caught an open SQLite fixture handle preventing Windows deletion;
closing the setup handle fixed the fixture without weakening no-stop assertions.

CLI discovery reads only an explicit adjacent installer-owned enrollment. It does
not initialize or migrate state. Only F explicitly authorizes Force anyway;
Cancel/Escape do not. Prompts never displace another permission gate and are
withdrawn when no longer pending. Transactional state checks bind a decision to
installation, prepared operation, consumed original intent, current agent lifetime,
and expiry. Submission or consumption is not process-exit or update-success proof.

Additional local UI regressions verify expiry while the prompt is open and no
automatic consent replay after uncertain submission. Three race-enabled repetitions
passed. These tests were added after the cited native run.

Remaining integration: the stable-launcher/versioned-child context handoff must
be explicit and validated, not inferred by searching parent directories. Full
initial update execution, launch/reconnect and real installation rehearsal are not
established by this matrix. All force effects tested here used owned fixture
callbacks; no live agent or command was stopped.
