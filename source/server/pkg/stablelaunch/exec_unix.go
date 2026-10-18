//go:build darwin || linux

package stablelaunch

import (
	"os"
	"syscall"
)

// exclusion.Acquire opens its file using os.OpenFile, which sets FD_CLOEXEC.
// Keep that shared lock through exec: successful replacement closes the lock
// descriptor, failure returns and Startup's defer releases it. No unlocked
// verification-to-exec gap and no parent lease survives into child startup.
// Exec also preserves the terminal, cwd, PID, normal signals, and explicitly
// inherited TUI restart descriptor without an intervening supervisor process.
func execute(p *prepared) (int, error) {
	err := syscall.Exec(p.target, append([]string{p.target}, p.args...), os.Environ())
	return 1, launchError(err)
}
