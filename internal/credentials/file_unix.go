//go:build linux || darwin

package credentials

import (
	"context"
	"errors"
	"os"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

func ensureDirectory(path string) error { return os.MkdirAll(path, 0700) }

func private(info os.FileInfo, _ string) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid()) && info.Mode().Perm()&0077 == 0
}

func privateRoot(root *os.Root) bool {
	info, err := root.Stat(".")
	return err == nil && private(info, "")
}

func openPrivate(root *os.Root, name string, flags int) (*os.File, error) {
	f, err := root.OpenFile(name, flags|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0600)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, os.ErrNotExist
		}
		return nil, errors.New("could not open the credential file safely")
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || !private(info, "") || info.Sys().(*syscall.Stat_t).Nlink != 1 {
		_ = f.Close()
		return nil, errors.New("credential files must be regular files owned by you with permissions 0600 and no links")
	}
	return f, nil
}

func lock(ctx context.Context, root *os.Root, name string) (func(), error) {
	f, err := openPrivate(root, name, os.O_CREATE|os.O_RDWR)
	if err != nil {
		return nil, err
	}
	for {
		err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return func() { _ = unix.Flock(int(f.Fd()), unix.LOCK_UN); _ = f.Close() }, nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
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

func syncDirectory(root *os.Root) error {
	f, err := root.Open(".")
	if err != nil {
		return errors.New("could not sync the credential directory")
	}
	defer func() { _ = f.Close() }()
	if err := f.Sync(); err != nil {
		return errors.New("could not sync the credential directory")
	}
	return nil
}

func replaceFile(root *os.Root, old, next string) error { return root.Rename(old, next) }
