//go:build !linux && !darwin && !windows

package credentials

import (
	"context"
	"errors"
)

func openKeyring(context.Context, string, bool) (Vault, func(), error) {
	return nil, nil, errors.New("OS keyring storage is not supported on this platform yet; use DEPLEXO_TOKEN")
}
