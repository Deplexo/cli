//go:build linux || darwin

package update

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func openRegular(root *os.Root, name string, flags int) (*os.File, error) {
	f, err := root.OpenFile(name, flags|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, errors.New("update path must be a regular file")
	}
	return f, nil
}

func acquireLock(root *os.Root) (*os.File, error) {
	f, err := openRegular(root, ".deplexo-update.lock", os.O_CREATE|os.O_RDWR)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

func replace(root *os.Root, stage, target, _ string) error {
	return root.Rename(stage, target)
}
