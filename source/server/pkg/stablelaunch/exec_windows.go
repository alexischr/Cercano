//go:build windows

package stablelaunch

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
)

// Windows has no exec replacement. The child shares the foreground console
// and inherits standard streams/cwd/environment. Do not detach or break away
// from the caller's job. The shared launch lease ends after process creation,
// never after Wait (which would deadlock later updates).
func execute(p *prepared) (int, error) {
	// Console interrupts reach both processes. Keep the waiting wrapper alive
	// while the child handles its own event; do not forward duplicate interrupts.
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)
	cmd := exec.Command(p.target, p.args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return 1, launchError(err)
	}
	// The process exists even if unlocking reports an error. Always reap it and
	// never return as if creation failed while abandoning a foreground child.
	lockErr := p.lock.Close()
	err := cmd.Wait()
	if lockErr != nil {
		return 1, lockErr
	}
	if err == nil {
		return 0, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), nil
	}
	return 1, err
}
