package credentials

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"time"
)

type keyringVault struct {
	ctx context.Context
	key string
}

func openKeyring(ctx context.Context, key string, noInput bool) (Vault, func(), error) {
	if noInput {
		return nil, nil, errors.New("macOS Keychain can open a desktop prompt; with --no-input, use DEPLEXO_TOKEN or --insecure-storage")
	}
	return &keyringVault{ctx: ctx, key: key}, func() {}, nil
}

func (v *keyringVault) run(input string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(v.ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/security", args...)
	cmd.Stdin = strings.NewReader(input)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 44 {
			return nil, ErrNotFound
		}
		return nil, errors.New("could not use macOS Keychain; unlock it or explicitly select --insecure-storage")
	}
	return out.Bytes(), nil
}

func (v *keyringVault) Load() (Session, error) {
	data, err := v.run("", "find-generic-password", "-s", "deplexo-cli", "-a", v.key, "-w")
	if err != nil {
		return Session{}, err
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
	if err != nil {
		return Session{}, ErrMalformed
	}
	return decode(decoded)
}

func (v *keyringVault) Save(s Session) error {
	data, err := json.Marshal(s)
	if err != nil {
		return errors.New("could not encode the sign-in")
	}
	// security's interactive parser accepts one command per line, limited to 4096 bytes.
	// Both substitutions contain only hex/base64 characters. Secrets go through stdin.
	command := "add-generic-password -U -s deplexo-cli -a " + v.key + " -w " + base64.StdEncoding.EncodeToString(data) + "\n"
	if len(command) > 4000 {
		return errors.New("sign-in exceeds the macOS Keychain command size limit")
	}
	if _, err := v.run(command, "-i"); err != nil {
		return err
	}
	stored, err := v.Load()
	if err != nil || stored.AccessToken != s.AccessToken || stored.RefreshToken != s.RefreshToken || stored.Pending != s.Pending {
		return errors.New("could not verify the saved Keychain sign-in")
	}
	return nil
}

func (v *keyringVault) Delete() error {
	_, err := v.run("", "delete-generic-password", "-s", "deplexo-cli", "-a", v.key)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}
