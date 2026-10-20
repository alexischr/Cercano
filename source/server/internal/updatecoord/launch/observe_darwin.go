//go:build darwin && cgo

package launch

/*
#include <sys/wait.h>
#include <signal.h>
#include <errno.h>
#include <string.h>
// WNOWAIT does not reap. ECHILD (including auto-reaped children) is refusal,
// never evidence of liveness. An exited unreaped child also refuses.
static int owned_child_alive(int pid) {
 struct sigaction action;
 if (sigaction(SIGCHLD, NULL, &action) != 0) return errno;
 // Without zombie retention, a numeric PID cannot pin our child lifetime.
 if (action.sa_handler == SIG_IGN || (action.sa_flags & SA_NOCLDWAIT)) return ECHILD;
 siginfo_t info;
 memset(&info, 0, sizeof(info));
 if (waitid(P_PID, (id_t)pid, &info, WEXITED | WNOHANG | WNOWAIT) != 0) return errno;
 return info.si_pid == 0 ? 0 : ESRCH;
}
*/
import "C"
import (
	"fmt"
	"os"
)

func observeLifetime(p *os.Process, observe func() error) error {
	alive := func() error {
		if e := C.owned_child_alive(C.int(p.Pid)); e != 0 {
			return fmt.Errorf("%w: child wait status %d", ErrObservation, int(e))
		}
		return nil
	}
	if e := alive(); e != nil {
		return e
	}
	if e := observe(); e != nil {
		return e
	}
	return alive()
}
