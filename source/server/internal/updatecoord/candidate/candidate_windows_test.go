//go:build windows

package candidate

import (
	"context"
	"golang.org/x/sys/windows"
	"os"
	"os/exec"
	"regexp"
	"testing"
	"time"
	"unsafe"
)

// Only the reexecuted fixture joins its own job. No inherited job policy or
// production launch flag is changed. Positive paths must pass, never skip.
func candidateTestHost(t *testing.T) bool {
	t.Helper()
	if os.Getenv("CERCANO_CANDIDATE_JOB") == "1" {
		job, e := windows.CreateJobObject(nil, nil)
		if e != nil {
			t.Fatal(e)
		}
		var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
		info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE | windows.JOB_OBJECT_LIMIT_BREAKAWAY_OK
		if _, e = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); e != nil {
			windows.CloseHandle(job)
			t.Fatal(e)
		}
		self, e := windows.GetCurrentProcess()
		if e != nil {
			windows.CloseHandle(job)
			t.Fatal(e)
		}
		if e = windows.AssignProcessToJobObject(job, self); e != nil {
			windows.CloseHandle(job)
			t.Fatal(e)
		}
		// Retained until helper exit: closing this handle here would kill us.
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	cmd := exec.CommandContext(ctx, exe, "-test.run=^"+regexp.QuoteMeta(t.Name())+"$", "-test.timeout=75s", "-test.v")
	cmd.Env = append(os.Environ(), "CERCANO_CANDIDATE_JOB=1")
	out, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatalf("owned candidate fixture: %v\n%s", e, out)
	}
	t.Logf("owned candidate fixture: %s", out)
	return true
}
