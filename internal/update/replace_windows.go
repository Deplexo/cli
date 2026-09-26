package update

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/sys/windows"
)

func openRegular(root *os.Root, name string, flags int) (*os.File, error) {
	before, err := root.Lstat(name)
	if err == nil && !before.Mode().IsRegular() {
		return nil, errors.New("update path must be a regular file")
	}
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if before == nil {
		flags |= os.O_EXCL
	}
	f, err := root.OpenFile(name, flags, 0600)
	if err != nil {
		return nil, err
	}
	after, err := f.Stat()
	current, pathErr := root.Lstat(name)
	if err != nil || pathErr != nil || !after.Mode().IsRegular() || !current.Mode().IsRegular() || !os.SameFile(current, after) || (before != nil && !os.SameFile(before, after)) {
		_ = f.Close()
		return nil, errors.New("update path changed while opening it")
	}
	return f, nil
}

func acquireLock(root *os.Root) (*os.File, error) {
	f, err := openRegular(root, ".deplexo-update.lock", os.O_CREATE|os.O_RDWR)
	if err != nil {
		return nil, err
	}
	if err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &windows.Overlapped{}); err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

type backupRecord struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

func cleanupBackup(root *os.Root, target string) error {
	receipt := "." + target + "-backup.json"
	f, err := openRegular(root, receipt, os.O_RDONLY)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	data, err := io.ReadAll(io.LimitReader(f, 4097))
	_ = f.Close()
	var record backupRecord
	if err != nil || len(data) > 4096 || json.Unmarshal(data, &record) != nil || !strings.HasPrefix(record.Name, "."+target+"-previous-") || strings.ContainsAny(record.Name, `/\`) || len(record.SHA256) != 64 {
		return errors.New("previous upgrade record is invalid; recover this installation with its installer")
	}
	old, err := openRegular(root, record.Name, os.O_RDONLY)
	if os.IsNotExist(err) {
		return root.Remove(receipt)
	}
	if err != nil {
		return err
	}
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(old, maxPackage+1))
	_ = old.Close()
	if err != nil || n > maxPackage || hex.EncodeToString(hash.Sum(nil)) != record.SHA256 {
		return errors.New("previous executable backup changed; preserve it and recover with the installer")
	}
	if err := root.Remove(record.Name); err != nil {
		return errors.New("close previous Deplexo processes before upgrading")
	}
	return root.Remove(receipt)
}

func replace(root *os.Root, stage, target, oldHash string) error {
	if err := cleanupBackup(root, target); err != nil {
		return err
	}
	// Windows permits renaming a running executable but not overwriting it.
	backup := "." + target + "-previous-" + rand.Text() + ".exe"
	receipt := "." + target + "-backup.json"
	recordStage := receipt + "-" + rand.Text()
	f, err := root.OpenFile(recordStage, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = root.Remove(recordStage) }()
	err = json.NewEncoder(f).Encode(backupRecord{Name: backup, SHA256: oldHash})
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return errors.New("could not save upgrade recovery information")
	}
	if err := root.Rename(recordStage, receipt); err != nil {
		return err
	}
	if err := root.Rename(target, backup); err != nil {
		_ = root.Remove(receipt)
		return err
	}
	if err := root.Rename(stage, target); err != nil {
		if rollback := root.Rename(backup, target); rollback != nil {
			return fmt.Errorf("upgrade and rollback failed; restore %s to %s: %w", backup, target, rollback)
		}
		_ = root.Remove(receipt)
		return err
	}
	// The next upgrade removes the recorded backup after the old process exits.
	if root.Remove(backup) == nil {
		_ = root.Remove(receipt)
	}
	return nil
}
