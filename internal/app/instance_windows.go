package app

import (
	"errors"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// privateSecurity builds a security descriptor for the current user and System.
func privateSecurity() (*windows.SecurityAttributes, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	sd, err := windows.SecurityDescriptorFromString("O:" + user.User.Sid.String() + "D:P(A;OICI;FA;;;" + user.User.Sid.String() + ")(A;OICI;FA;;;SY)")
	if err != nil {
		return nil, err
	}
	return &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}, nil
}

// checkPrivateSecurity validates that only the current user and System have access.
func checkPrivateSecurity(sd *windows.SECURITY_DESCRIPTOR) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	owner, _, err := sd.Owner()
	if err != nil || owner == nil || !owner.Equals(user.User.Sid) {
		return errors.New("instance storage must be owned by the current user")
	}
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil || dacl.AceCount == 0 {
		return errors.New("instance storage requires a private ACL")
	}
	for n := uint32(0); n < uint32(dacl.AceCount); n++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, n, &ace); err != nil {
			return err
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			return errors.New("unsupported instance storage ACL")
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if !sid.Equals(user.User.Sid) && !sid.IsWellKnown(windows.WinLocalSystemSid) {
			return errors.New("instance storage ACL must allow only the current user and System")
		}
	}
	return nil
}

// privateDirectory creates protected storage and rejects reparse-point directories.
func privateDirectory(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	sa, err := privateSecurity()
	if err != nil {
		return err
	}
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	if err := windows.CreateDirectory(p, sa); err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return err
	}
	attrs, err := windows.GetFileAttributes(p)
	if err != nil {
		return err
	}
	if attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || attrs&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
		return errors.New("instance directory must not be a reparse point")
	}
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	return checkPrivateSecurity(sd)
}

// privateFile opens a regular file with a private ACL.
func privateFile(path string) (*os.File, error) {
	return openPrivateFile(path, true)
}

// openPrivateFile opens checked storage, optionally creating it for the instance lock.
func openPrivateFile(path string, create bool) (*os.File, error) {
	sa, err := privateSecurity()
	if err != nil {
		return nil, err
	}
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	access, disposition := uint32(windows.GENERIC_READ|windows.READ_CONTROL), uint32(windows.OPEN_EXISTING)
	if create {
		access |= windows.GENERIC_WRITE
		disposition = windows.OPEN_ALWAYS
	}
	h, err := windows.CreateFile(p, access, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, sa, disposition, windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(h), path)
	if err := checkPrivateFile(f); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

// checkPrivateFile rejects nonprivate, linked or nonregular storage files.
func checkPrivateFile(f *os.File) error {
	h := windows.Handle(f.Fd())
	var info windows.ByHandleFileInformation
	err := windows.GetFileInformationByHandle(h, &info)
	if err != nil || info.FileAttributes&(windows.FILE_ATTRIBUTE_REPARSE_POINT|windows.FILE_ATTRIBUTE_DIRECTORY) != 0 || info.NumberOfLinks != 1 {
		return errors.New("storage file must be a regular file, not a link or reparse point")
	}
	sd, err := windows.GetSecurityInfo(h, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err == nil {
		err = checkPrivateSecurity(sd)
	}
	return err
}

// lockFile locks byte zero without blocking.
func lockFile(f *os.File) (bool, error) {
	err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &windows.Overlapped{})
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return false, nil
	}
	return err == nil, err
}
