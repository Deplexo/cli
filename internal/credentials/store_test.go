package credentials

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	return &Store{Directory: filepath.Join(t.TempDir(), "credentials"), Origin: "https://example.com", Profile: "default", Insecure: true}
}
func testSession() Session {
	return Session{Version: 1, Origin: "https://example.com", Profile: "default", AccountID: "account-one", AccessToken: "test-access", RefreshToken: "test-refresh", ExpiresAt: time.Now().Add(time.Hour)}
}

func TestFileRoundTripAndIsolation(t *testing.T) {
	s := testStore(t)
	if err := s.WithLock(context.Background(), func(v Vault) error {
		if _, err := v.Load(); !errors.Is(err, ErrNotFound) {
			t.Fatalf("missing load: %v", err)
		}
		if err := v.Save(testSession()); err != nil {
			return err
		}
		got, err := v.Load()
		if err != nil {
			return err
		}
		if got.RefreshToken != "test-refresh" {
			t.Fatal("lost refresh token")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ origin, profile string }{{"https://other.example", "default"}, {s.Origin, "other"}} {
		other := *s
		other.Origin, other.Profile = tc.origin, tc.profile
		if err := other.WithLock(context.Background(), func(v Vault) error {
			_, err := v.Load()
			if !errors.Is(err, ErrNotFound) {
				return fmt.Errorf("session crossed profiles: %v", err)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.WithLock(context.Background(), func(v Vault) error {
		if err := v.Delete(); err != nil {
			return err
		}
		_, err := v.Load()
		if !errors.Is(err, ErrNotFound) {
			t.Fatal(err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRejectCredentialLinksAndPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows ACL checks run in TestWindowsCredentialACL")
	}
	s := testStore(t)
	if err := s.WithLock(context.Background(), func(v Vault) error { return v.Save(testSession()) }); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(s.Directory, "*.json"))
	if err != nil || len(files) != 1 {
		t.Fatalf("credential files %v %v", files, err)
	}
	if err := os.Chmod(files[0], 0644); err != nil {
		t.Fatal(err)
	}
	if err := s.WithLock(context.Background(), func(v Vault) error { _, err := v.Load(); return err }); err == nil {
		t.Fatal("accepted world-readable credential")
	}
	if err := os.Remove(files[0]); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(target, []byte(`{"version":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, files[0]); err != nil {
		t.Fatal(err)
	}
	if err := s.WithLock(context.Background(), func(v Vault) error { _, err := v.Load(); return err }); err == nil {
		t.Fatal("followed credential symlink")
	}
	// Atomic replacement may replace a symlink but must never modify its destination.
	if err := s.WithLock(context.Background(), func(v Vault) error { return v.Save(testSession()) }); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(target)
	if string(data) != `{"version":1}` {
		t.Fatal("modified symlink destination")
	}
}

func TestLockCancellation(t *testing.T) {
	s := testStore(t)
	err := s.WithLock(context.Background(), func(Vault) error {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		err := s.WithLock(ctx, func(Vault) error { t.Error("second lock acquired"); return nil })
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("lock cancellation: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestProcessLockHelper(t *testing.T) {
	path := os.Getenv("DEPLEXO_TEST_LOCK_DIR")
	if path == "" {
		return
	}
	s := &Store{Directory: path, Origin: "https://example.com", Profile: "default", Insecure: true}
	err := s.WithLock(context.Background(), func(v Vault) error {
		if os.Getenv("DEPLEXO_TEST_CRASH") == "1" {
			os.Exit(23)
		}
		value, err := v.Load()
		if errors.Is(err, ErrNotFound) {
			value = testSession()
			value.AccountID = ""
		} else if err != nil {
			return err
		}
		value.AccountID += "x"
		time.Sleep(20 * time.Millisecond)
		return v.Save(value)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestProcessLockAndCrashRecovery(t *testing.T) {
	s := testStore(t)
	newProcess := func(crash bool) *exec.Cmd {
		cmd := exec.Command(os.Args[0], "-test.run=^TestProcessLockHelper$")
		cmd.Env = append(os.Environ(), "DEPLEXO_TEST_LOCK_DIR="+s.Directory)
		if crash {
			cmd.Env = append(cmd.Env, "DEPLEXO_TEST_CRASH=1")
		}
		return cmd
	}
	crash := newProcess(true)
	if err := crash.Run(); err == nil {
		t.Fatal("helper did not crash")
	}
	var processes []*exec.Cmd
	for range 4 {
		cmd := newProcess(false)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		processes = append(processes, cmd)
	}
	for _, cmd := range processes {
		if err := cmd.Wait(); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.WithLock(context.Background(), func(v Vault) error {
		value, err := v.Load()
		if err != nil {
			return err
		}
		if value.AccountID != "xxxx" {
			t.Fatalf("lost process update: %q", value.AccountID)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestNativeKeyring(t *testing.T) {
	if os.Getenv("DEPLEXO_TEST_NATIVE_KEYRING") != "1" {
		t.Skip("set DEPLEXO_TEST_NATIVE_KEYRING=1 with an isolated unlocked native keyring")
	}
	s := testStore(t)
	s.Insecure = false
	s.Profile = fmt.Sprintf("test-%d", time.Now().UnixNano())
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	err := s.WithLock(ctx, func(v Vault) error {
		defer func() {
			if err := v.Delete(); err != nil {
				t.Error(err)
			}
		}()
		value := testSession()
		value.Profile = s.Profile
		if err := v.Save(value); err != nil {
			return err
		}
		got, err := v.Load()
		if err != nil {
			return err
		}
		if got.RefreshToken != value.RefreshToken {
			t.Fatal("keyring round trip failed")
		}
		value.Pending = true
		value.AccessToken = ""
		value.RefreshToken = ""
		if err := v.Save(value); err != nil {
			return err
		}
		got, err = v.Load()
		if err != nil {
			return err
		}
		if !got.Pending || got.RefreshToken != "" {
			t.Fatal("keyring replacement failed")
		}
		if err := v.Delete(); err != nil {
			return err
		}
		_, err = v.Load()
		if !errors.Is(err, ErrNotFound) {
			return fmt.Errorf("deleted credential remains: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMalformedCredential(t *testing.T) {
	for _, data := range []string{`not json`, `{"version":2}`, strings.Repeat("x", 16385)} {
		if _, err := decode([]byte(data)); !errors.Is(err, ErrMalformed) {
			t.Fatalf("malformed: %v", err)
		}
	}
}

func TestOversizedCredentialCanBeRemoved(t *testing.T) {
	s := testStore(t)
	err := s.WithLock(context.Background(), func(v Vault) error {
		file := v.(*fileVault)
		f, err := openPrivate(file.root, file.name, os.O_CREATE|os.O_WRONLY|os.O_EXCL)
		if err != nil {
			return err
		}
		_, writeErr := f.WriteString(strings.Repeat("x", 16385))
		closeErr := f.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
		if _, err := v.Load(); !errors.Is(err, ErrMalformed) {
			t.Fatalf("oversized file: %v", err)
		}
		if err := v.Delete(); err != nil {
			return err
		}
		if _, err := v.Load(); !errors.Is(err, ErrNotFound) {
			t.Fatalf("deleted oversized file remains: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
