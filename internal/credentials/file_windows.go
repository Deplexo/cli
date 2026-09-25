package credentials

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func currentSID() (*windows.SID, error) {
	u, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	return u.User.Sid, nil
}

func ensureDirectory(path string) error {
	if _, err := os.Lstat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := ensureDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	sid, err := currentSID()
	if err != nil {
		return err
	}
	sd, err := windows.SecurityDescriptorFromString("O:" + sid.String() + "D:P(A;OICI;FA;;;" + sid.String() + ")")
	if err != nil {
		return err
	}
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	sa := &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	err = windows.CreateDirectory(p, sa)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return nil
	}
	return err
}

func privateHandle(h windows.Handle) bool {
	sid, err := currentSID()
	if err != nil {
		return false
	}
	sd, err := windows.GetSecurityInfo(h, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return false
	}
	owner, _, err := sd.Owner()
	if err != nil || !owner.Equals(sid) {
		return false
	}
	acl, _, err := sd.DACL()
	if err != nil || acl == nil || acl.AceCount == 0 {
		return false
	}
	for i := uint32(0); i < uint32(acl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if windows.GetAce(acl, i, &ace) != nil || ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			return false
		}
		if !(*windows.SID)(unsafe.Pointer(&ace.SidStart)).Equals(sid) {
			return false
		}
	}
	return true
}

func private(info os.FileInfo, path string) bool {
	if info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	return privateHandle(windows.Handle(f.Fd()))
}

func privateRoot(root *os.Root) bool {
	f, err := root.Open(".")
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	return privateHandle(windows.Handle(f.Fd()))
}

func openPrivate(root *os.Root, name string, flags int) (*os.File, error) {
	before, err := root.Lstat(name)
	if err == nil && !before.Mode().IsRegular() {
		return nil, errors.New("credential path must be a regular file without links")
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	f, err := root.OpenFile(name, flags, 0600)
	if err != nil {
		return nil, err
	}
	after, err := f.Stat()
	var information windows.ByHandleFileInformation
	if err != nil || !after.Mode().IsRegular() || (before != nil && !os.SameFile(before, after)) || !privateHandle(windows.Handle(f.Fd())) || windows.GetFileInformationByHandle(windows.Handle(f.Fd()), &information) != nil || information.NumberOfLinks != 1 || information.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		_ = f.Close()
		return nil, errors.New("credential file must be owned by you, allow access only to your account, and have no links")
	}
	return f, nil
}

func lock(ctx context.Context, root *os.Root, name string) (func(), error) {
	f, err := openPrivate(root, name, os.O_CREATE|os.O_RDWR)
	if err != nil {
		return nil, err
	}
	overlap := &windows.Overlapped{}
	for {
		err = windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, overlap)
		if err == nil {
			return func() { _ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, overlap); _ = f.Close() }, nil
		}
		if !errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			_ = f.Close()
			return nil, errors.New("could not lock the stored sign-in")
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// Windows does not support syncing a directory handle. File contents are flushed before replacement.
func syncDirectory(*os.Root) error { return nil }

func replaceFile(root *os.Root, old, next string) error { return root.Rename(old, next) }
