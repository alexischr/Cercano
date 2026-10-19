// Package nativeprocess provides read-only kernel process observations.
// Observations are not authentication, unique endpoint ownership, or update authority.
package nativeprocess

// Identity is obtained from the kernel, not from mutable argv[0] or PATH.
// Start time distinguishes a process from a later reuse of the same PID.
type Identity struct {
	PID        int
	UID        uint32
	Executable string
	// StartTicks is Linux boot-relative clock ticks; macOS uses the fields below.
	StartTicks        uint64
	StartSeconds      uint64
	StartMicroseconds uint64
	// WindowsSID is the user SID of the process token, in SDDL string form
	// (for example "S-1-5-21-..."). It is deliberately not folded into UID:
	// Unix UIDs and Windows SIDs have no meaningful numeric equivalence.
	WindowsSID string
	// WindowsStartFileTime is the process creation time as a raw Windows
	// FILETIME: 100-nanosecond intervals since 1601-01-01 UTC. It is
	// deliberately not folded into the Unix start-time fields.
	WindowsStartFileTime int64
}

func (p Identity) SameProcess(other Identity) bool {
	return p.PID == other.PID && p.UID == other.UID &&
		p.StartTicks == other.StartTicks && p.StartSeconds == other.StartSeconds && p.StartMicroseconds == other.StartMicroseconds &&
		p.WindowsSID == other.WindowsSID && p.WindowsStartFileTime == other.WindowsStartFileTime &&
		p.Executable == other.Executable
}
