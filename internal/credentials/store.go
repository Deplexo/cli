package credentials

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"
)

var ErrNotFound = errors.New("no stored sign-in")
var ErrMalformed = errors.New("stored sign-in is invalid; run `deplexo auth logout` before signing in again")

type Session struct {
	Version      int       `json:"version"`
	Origin       string    `json:"origin"`
	Profile      string    `json:"profile"`
	AccountID    string    `json:"account_id"`
	Email        string    `json:"email"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	Scopes       []string  `json:"scopes"`
	ExpiresAt    time.Time `json:"expires_at"`
	Pending      bool      `json:"pending"`
}

type Vault interface {
	Load() (Session, error)
	Save(Session) error
	Delete() error
}

type Store struct {
	Directory, Origin, Profile string
	Insecure                   bool
	NoInput                    bool
}

func (s *Store) WithLock(ctx context.Context, fn func(Vault) error) error {
	root, err := privateDirectory(s.Directory)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	sum := sha256.Sum256([]byte(s.Origin + "\x00" + s.Profile))
	key := hex.EncodeToString(sum[:])
	unlock, err := lock(ctx, root, key+".lock")
	if err != nil {
		return err
	}
	defer unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.Insecure {
		return fn(&fileVault{root: root, name: key + ".json"})
	}
	// Complete local mutations while holding the lock, even if a remote request was cancelled.
	// Each native keyring call has its own bounded deadline.
	vault, closeVault, err := openKeyring(context.WithoutCancel(ctx), key, s.NoInput)
	if err != nil {
		return err
	}
	defer closeVault()
	return fn(vault)
}

func privateDirectory(path string) (*os.Root, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("credential directory must use an absolute path")
	}
	if err := ensureDirectory(path); err != nil {
		return nil, errors.New("could not create the credential directory")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || !private(info, path) {
		return nil, errors.New("credential directory must be owned by you, allow access only to your account, and must not be a symlink")
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, errors.New("could not open the credential directory")
	}
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(info, opened) || !privateRoot(root) {
		_ = root.Close()
		return nil, errors.New("credential directory changed while opening it")
	}
	return root, nil
}

type fileVault struct {
	root *os.Root
	name string
}

func (v *fileVault) Load() (Session, error) {
	f, err := openPrivate(v.root, v.name, os.O_RDONLY)
	if errors.Is(err, os.ErrNotExist) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, 16385))
	if err != nil {
		return Session{}, errors.New("could not read the credential file")
	}
	if len(data) > 16384 {
		return Session{}, ErrMalformed
	}
	return decode(data)
}

func decode(data []byte) (Session, error) {
	var s Session
	if len(data) > 16384 || json.Unmarshal(data, &s) != nil || s.Version != 1 {
		return s, ErrMalformed
	}
	return s, nil
}

func (v *fileVault) Save(s Session) error {
	data, err := json.Marshal(s)
	if err != nil || len(data) > 16384 {
		return errors.New("could not encode the sign-in")
	}
	// The lock serializes writes. Exclusive creation detects stale files and symlinks.
	name := v.name + ".tmp"
	if err := v.root.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
		return errors.New("could not clear the previous credential write")
	}
	f, err := openPrivate(v.root, name, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if err != nil {
		return err
	}
	defer func() { _ = v.root.Remove(name) }()
	_, writeErr := f.Write(data)
	syncErr := f.Sync()
	closeErr := f.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return errors.New("could not save the sign-in")
	}
	if err := replaceFile(v.root, name, v.name); err != nil {
		return errors.New("could not replace the stored sign-in")
	}
	return syncDirectory(v.root)
}

func (v *fileVault) Delete() error {
	if err := v.root.Remove(v.name); err != nil && !errors.Is(err, os.ErrNotExist) {
		return errors.New("could not remove the stored sign-in")
	}
	return syncDirectory(v.root)
}
