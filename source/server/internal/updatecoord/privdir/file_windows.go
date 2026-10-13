//go:build windows

package privdir

import (
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
	"runtime"
	"unsafe"
)

func CreatePrivateFile(path string) (*os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, ErrInvalidPath
	}
	if e := VerifyExisting(filepath.Dir(path)); e != nil {
		return nil, e
	}
	id, e := currentIdentity()
	if e != nil {
		return nil, e
	}
	sd, e := creationDescriptor(id)
	if e != nil {
		return nil, e
	}
	sa := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	p, e := windows.UTF16PtrFromString(path)
	if e != nil {
		return nil, e
	}
	h, e := windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE|windows.READ_CONTROL, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, &sa, windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL, 0)
	runtime.KeepAlive(sd)
	if e != nil {
		return nil, e
	}
	return os.NewFile(uintptr(h), path), nil
}
func OpenPrivateFile(path string) (*os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, ErrInvalidPath
	}
	p, e := windows.UTF16PtrFromString(path)
	if e != nil {
		return nil, e
	}
	h, e := windows.CreateFile(p, windows.GENERIC_READ|windows.READ_CONTROL, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if e != nil {
		return nil, e
	}
	f := os.NewFile(uintptr(h), path)
	fail := func(e error) (*os.File, error) { f.Close(); return nil, e }
	var info windows.ByHandleFileInformation
	if e = windows.GetFileInformationByHandle(h, &info); e != nil {
		return fail(e)
	}
	if info.FileAttributes&(windows.FILE_ATTRIBUTE_REPARSE_POINT|windows.FILE_ATTRIBUTE_DIRECTORY) != 0 || info.NumberOfLinks != 1 {
		return fail(ErrUnsafePath)
	}
	id, e := currentIdentity()
	if e != nil {
		return fail(e)
	}
	policy, e := newAccessPolicy(id)
	if e != nil {
		return fail(e)
	}
	sd, e := windows.GetSecurityInfo(h, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if e != nil {
		return fail(e)
	}
	owner, _, e := sd.Owner()
	if e != nil || !policy.ownerTrusted(owner) {
		return fail(ErrForeignOwner)
	}
	if e = policy.verifyDACL(sd); e != nil {
		return fail(e)
	}
	return f, nil
}
