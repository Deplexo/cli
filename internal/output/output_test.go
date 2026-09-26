package output

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/Deplexo/cli/internal/api"
)

func TestTerminalControlsEscaped(t *testing.T) {
	input := "name\x1b]52;c;payload\a\r\n\t\u202eend"
	got := Safe(input)
	for _, control := range []string{"\x1b", "\a", "\r", "\n", "\t", "\u202e"} {
		if strings.Contains(got, control) {
			t.Errorf("control survives: %q", got)
		}
	}
	if !strings.Contains(got, `\u001b`) {
		t.Fatal("escape sequence not visible")
	}
}

func TestCancelledMutationKeepsOutcomeWarning(t *testing.T) {
	var out bytes.Buffer
	err := &api.MutationError{Cause: context.Canceled}
	Report(&out, err, false)
	if ExitCode(err) != 130 || !strings.Contains(out.String(), "may still complete") {
		t.Fatalf("cancellation outcome: %s", out.String())
	}
}
