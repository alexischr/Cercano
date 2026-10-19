//go:build windows

package nativeprocess

import (
	"os"
	"testing"

	"golang.org/x/sys/windows"
)

// windowsEpochToUnixOffset is the number of 100-nanosecond FILETIME
// intervals between 1601-01-01 UTC (the FILETIME epoch) and 1970-01-01 UTC
// (the Unix epoch). x/sys windows.Filetime.Nanoseconds is defined as
// (raw - windowsEpochToUnixOffset) * 100, so its value differs from the raw
// FILETIME in both epoch base and scale.
const windowsEpochToUnixOffset = int64(116444736000000000)

// rawFileTime is the value WindowsStartFileTime must hold: the raw 64-bit
// FILETIME word, not a Unix-epoch conversion of it.
func rawFileTime(ft windows.Filetime) uint64 {
	return uint64(ft.HighDateTime)<<32 | uint64(ft.LowDateTime)
}

// TestWindowsStartFileTimeStoresRawFileTime is the regression test for a bug
// where WindowsStartFileTime was stored as creation.Nanoseconds(): a
// Unix-epoch, nanosecond-scale conversion that silently changed the identity
// value and lost the raw kernel word. It inspects the test process, reads the
// creation FILETIME independently, and requires the identity field to equal
// the raw word and to differ from the Nanoseconds() conversion.
func TestWindowsStartFileTimeStoresRawFileTime(t *testing.T) {
	pid := os.Getpid()
	id, err := Inspect(pid)
	if err != nil {
		t.Fatalf("Inspect(%d): %v", pid, err)
	}
	if id.WindowsStartFileTime == 0 {
		t.Fatal("WindowsStartFileTime is zero")
	}

	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		t.Fatalf("OpenProcess(%d): %v", pid, err)
	}
	defer windows.CloseHandle(handle)
	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(handle, &creation, &exit, &kernel, &user); err != nil {
		t.Fatalf("GetProcessTimes(%d): %v", pid, err)
	}

	raw := rawFileTime(creation)
	if id.WindowsStartFileTime != raw {
		t.Fatalf("WindowsStartFileTime = %d, want raw FILETIME %d", id.WindowsStartFileTime, raw)
	}

	// Distinction from Filetime.Nanoseconds(): re-based to the Unix epoch
	// and rescaled to nanoseconds, so for any process created after 1970 it
	// is a different number than the raw word.
	ns := creation.Nanoseconds()
	if ns <= 0 {
		t.Fatalf("process created after 1970 must convert to positive nanoseconds, got %d", ns)
	}
	if uint64(ns) == id.WindowsStartFileTime {
		t.Fatalf("WindowsStartFileTime %d matches Filetime.Nanoseconds(); raw FILETIME and the Unix-epoch conversion must be distinct values", id.WindowsStartFileTime)
	}
	// The exact x/sys relation, proving both readings are the same kernel
	// fact expressed in two different units.
	if want := (ns/100 + windowsEpochToUnixOffset); int64(raw) != want {
		t.Fatalf("raw FILETIME %d and Nanoseconds %d disagree with the x/sys relation (want raw %d)", raw, ns, want)
	}
}

// TestWindowsFiletimeNanosecondsDiffersFromRawFileTime is deterministic: it
// pins the x/sys conversion and the raw word for two known instants, so the
// distinction cannot depend on a live process.
func TestWindowsFiletimeNanosecondsDiffersFromRawFileTime(t *testing.T) {
	// 1970-01-01T00:00:00Z: the Unix epoch. Raw FILETIME is the epoch
	// offset itself; Nanoseconds() is zero — the two representations agree
	// on nothing about the underlying word.
	epoch := windows.NsecToFiletime(0)
	if got, want := rawFileTime(epoch), uint64(windowsEpochToUnixOffset); got != want {
		t.Fatalf("Unix epoch raw FILETIME = %d, want %d", got, want)
	}
	if got := epoch.Nanoseconds(); got != 0 {
		t.Fatalf("Unix epoch Nanoseconds() = %d, want 0", got)
	}

	// 2024-01-01T00:00:00Z: a modern creation-time-shaped instant where the
	// two representations are numerically distinct.
	modern := windows.NsecToFiletime(1704067200_000000000)
	raw := rawFileTime(modern)
	ns := modern.Nanoseconds()
	if ns != 1704067200_000000000 {
		t.Fatalf("2024-01-01 Nanoseconds() = %d, want 1704067200000000000", ns)
	}
	if uint64(ns) == raw {
		t.Fatalf("Nanoseconds %d equals raw FILETIME %d; representations must differ", ns, raw)
	}
	if raw != 133485408000000000 {
		t.Fatalf("2024-01-01 raw FILETIME = %d, want 133485408000000000", raw)
	}
}
