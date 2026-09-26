//go:build !linux && !darwin && !windows

package credentials

import (
	"context"
	"errors"
	"os"
)

var errPlatform = errors.New("stored sign-ins are not supported on this platform yet; use DEPLEXO_TOKEN")

func privateRoot(*os.Root) bool { return false }

func ensureDirectory(string) error                           { return errPlatform }
func private(os.FileInfo, string) bool                       { return false }
func openPrivate(*os.Root, string, int) (*os.File, error)    { return nil, errPlatform }
func lock(context.Context, *os.Root, string) (func(), error) { return nil, errPlatform }
func syncDirectory(*os.Root) error                           { return errPlatform }

func replaceFile(*os.Root, string, string) error { return errPlatform }
