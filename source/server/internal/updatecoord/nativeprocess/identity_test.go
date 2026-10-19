package nativeprocess

import "testing"

// TestSameProcessIncludesWindowsFields is portable: it runs on every platform
// so that the Windows identity fields can never be silently dropped from the
// comparison on Unix, and vice versa.
func TestSameProcessIncludesWindowsFields(t *testing.T) {
	base := Identity{
		PID:                  42,
		Executable:           `C:\Program Files\Cercano\cercano.exe`,
		WindowsSID:           "S-1-5-21-1004336348-117809651-682003330-1001",
		WindowsStartFileTime: 133679424000000000,
	}
	if !base.SameProcess(base) {
		t.Fatal("identity must match itself")
	}
	mutations := map[string]func(*Identity){
		"PID":                  func(p *Identity) { p.PID = 43 },
		"UID":                  func(p *Identity) { p.UID = 1 },
		"Executable":           func(p *Identity) { p.Executable = `C:\Program Files\Cercano\other.exe` },
		"StartTicks":           func(p *Identity) { p.StartTicks = 1 },
		"StartSeconds":         func(p *Identity) { p.StartSeconds = 1 },
		"StartMicroseconds":    func(p *Identity) { p.StartMicroseconds = 1 },
		"WindowsSID":           func(p *Identity) { p.WindowsSID = "S-1-5-21-1004336348-117809651-682003330-999" },
		"WindowsStartFileTime": func(p *Identity) { p.WindowsStartFileTime++ },
	}
	for name, mutate := range mutations {
		other := base
		mutate(&other)
		if other.SameProcess(base) || base.SameProcess(other) {
			t.Fatalf("SameProcess must include the %s field", name)
		}
	}
}

// TestSameProcessKeepsUnixAndWindowsIdentitiesDistinct pins the decision not
// to overload the Unix UID and start-time fields on Windows: a Windows
// identity and a Unix-shaped identity never compare equal, so a probe built
// for one platform cannot masquerade as the other.
func TestSameProcessKeepsUnixAndWindowsIdentitiesDistinct(t *testing.T) {
	windowsShaped := Identity{
		PID:                  42,
		Executable:           "/bin/cercano",
		WindowsSID:           "S-1-5-21-1-2-3-4",
		WindowsStartFileTime: 133679424000000000,
	}
	unixShaped := Identity{PID: 42, UID: 1000, Executable: "/bin/cercano", StartTicks: 123}
	if windowsShaped.SameProcess(unixShaped) || unixShaped.SameProcess(windowsShaped) {
		t.Fatal("Unix UID/time fields and Windows SID/FILETIME fields must not be interchangeable")
	}
	if (windowsShaped == unixShaped) != (windowsShaped.SameProcess(unixShaped)) {
		t.Fatal("SameProcess must agree with struct equality for populated identities")
	}
}
