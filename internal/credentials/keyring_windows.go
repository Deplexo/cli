package credentials

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/danieljoos/wincred"
	"golang.org/x/sys/windows"
)

type keyringVault struct{ key string }

func openKeyring(ctx context.Context, key string, _ bool) (Vault, func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	return &keyringVault{key: "deplexo-cli:" + key}, func() {}, nil
}

func (v *keyringVault) Load() (Session, error) {
	cred, err := wincred.GetGenericCredential(v.key)
	if errors.Is(err, windows.ERROR_NOT_FOUND) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, errors.New("could not read Windows Credential Manager")
	}
	return decode(cred.CredentialBlob)
}

func (v *keyringVault) Save(s Session) error {
	data, err := json.Marshal(s)
	if err != nil || len(data) > 2560 {
		return errors.New("sign-in exceeds the Windows Credential Manager size limit")
	}
	cred := wincred.NewGenericCredential(v.key)
	cred.CredentialBlob = data
	if err := cred.Write(); err != nil {
		return errors.New("could not save the sign-in in Windows Credential Manager")
	}
	return nil
}

func (v *keyringVault) Delete() error {
	cred, err := wincred.GetGenericCredential(v.key)
	if errors.Is(err, windows.ERROR_NOT_FOUND) {
		return nil
	}
	if err != nil || cred.Delete() != nil {
		return errors.New("could not remove the sign-in from Windows Credential Manager")
	}
	return nil
}
