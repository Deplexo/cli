package credentials

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWindowsCredentialACL(t *testing.T) {
	s := testStore(t)
	if err := s.WithLock(context.Background(), func(v Vault) error { return v.Save(testSession()) }); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(s.Directory, "*.json"))
	if err != nil || len(files) != 1 {
		t.Fatal("missing credential")
	}
	info, err := os.Stat(files[0])
	if err != nil {
		t.Fatal(err)
	}
	if !private(info, files[0]) {
		t.Fatal("new file ACL is not private")
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	acl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(files[0], windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.WithLock(context.Background(), func(v Vault) error { _, err := v.Load(); return err }); err == nil {
		t.Fatal("accepted world-readable ACL")
	}
}
