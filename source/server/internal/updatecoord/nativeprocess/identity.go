// Package nativeprocess provides read-only kernel process observations.
// Observations are not authentication, unique endpoint ownership, or update authority.
package nativeprocess

// Identity is obtained from the kernel, not from mutable argv[0] or PATH.
// Start time distinguishes a process from a later reuse of the same PID.
type Identity struct {
	PID               int
	UID               uint32
	Executable        string
	StartSeconds      uint64
	StartMicroseconds uint64
}

func (p Identity) SameProcess(other Identity) bool {
	return p.PID == other.PID && p.UID == other.UID &&
		p.StartSeconds == other.StartSeconds && p.StartMicroseconds == other.StartMicroseconds &&
		p.Executable == other.Executable
}
