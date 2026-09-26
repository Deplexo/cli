package credentials

import (
	"context"
	"strings"
	"testing"
)

func TestNoInputDoesNotInvokeKeychain(t *testing.T) {
	_, _, err := openKeyring(context.Background(), "test-profile", true)
	if err == nil || !strings.Contains(err.Error(), "--no-input") {
		t.Fatalf("no-input Keychain access: %v", err)
	}
}
