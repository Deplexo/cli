package output

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"

	"github.com/Deplexo/cli/internal/api"
)

type Error struct {
	Code    int
	Message string
	cause   error
}

func (e *Error) Error() string        { return e.Message }
func (e *Error) Unwrap() error        { return e.cause }
func Usage(message string) error      { return &Error{Code: 2, Message: message} }
func SignIn(message string) error     { return &Error{Code: 3, Message: message} }
func Permission(message string) error { return &Error{Code: 4, Message: message} }
func Interrupted(message string) error {
	return &Error{Code: 130, Message: message, cause: context.Canceled}
}

func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	if errors.Is(err, context.Canceled) {
		return 130
	}
	var own *Error
	if errors.As(err, &own) {
		return own.Code
	}
	var remote *api.Error
	if errors.As(err, &remote) {
		if remote.Status == 401 {
			return 3
		}
		if remote.Status == 403 {
			return 4
		}
	}
	return 1
}

func Safe(s string) string {
	var out strings.Builder
	for _, r := range s {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '\u2028' || r == '\u2029' {
			fmt.Fprintf(&out, "\\u%04x", r)
		} else {
			out.WriteRune(r)
		}
	}
	return out.String()
}

func JSON(w io.Writer, value any) error { return json.NewEncoder(w).Encode(value) }

func Report(w io.Writer, err error, jsonOutput bool) {
	NewPrinter(w, "auto", nil).Report(err, jsonOutput)
}

func (p *Printer) Report(err error, jsonOutput bool) {
	message := err.Error()
	if errors.Is(err, context.Canceled) {
		message = "interrupted"
		var own *Error
		if errors.As(err, &own) {
			message = own.Message
		}
		var mutation *api.MutationError
		if errors.As(err, &mutation) {
			message = "interrupted; the operation may still complete on the server; check the dashboard before retrying"
		}
	}
	if jsonOutput {
		_ = JSON(p.w, struct {
			Error    string `json:"error"`
			ExitCode int    `json:"exit_code"`
		}{message, ExitCode(err)})
	} else {
		p.Error(message)
	}
}
