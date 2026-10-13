//go:build windows

package privdir

import (
	"golang.org/x/sys/windows"
	"path/filepath"
	"testing"
)

func TestPrivateFileRefusesUnsafeACLWithoutRepair(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "private")
	if _, e := Ensure(dir); e != nil {
		t.Fatal(e)
	}
	p := filepath.Join(dir, "identity")
	f, e := CreatePrivateFile(p)
	if e != nil {
		t.Fatal(e)
	}
	f.Close()
	sd, e := windows.SecurityDescriptorFromString("D:P(A;;FA;;;WD)")
	if e != nil {
		t.Fatal(e)
	}
	acl, _, e := sd.DACL()
	if e != nil {
		t.Fatal(e)
	}
	if e = windows.SetNamedSecurityInfo(p, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); e != nil {
		t.Fatal(e)
	}
	before, e := windows.GetNamedSecurityInfo(p, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if e != nil {
		t.Fatal(e)
	}
	if f, e = OpenPrivateFile(p); e == nil {
		f.Close()
		t.Fatal("world-accessible file accepted")
	}
	after, e := windows.GetNamedSecurityInfo(p, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if e != nil {
		t.Fatal(e)
	}
	if before.String() != after.String() {
		t.Fatal("unsafe ACL was repaired")
	}
}
