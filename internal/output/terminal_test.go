package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
)

func TestColorPolicyAndMachineOutput(t *testing.T) {
	for _, tc := range []struct {
		name, mode, noColor, term string
		wantColor                 bool
	}{
		{"redirected", "auto", "", "xterm", false},
		{"forced", "always", "", "xterm", true},
		{"disabled", "never", "", "xterm", false},
		{"no-color", "always", "1", "xterm", false},
		{"dumb", "always", "", "dumb", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			p := NewPrinter(&out, tc.mode, func(key string) (string, bool) {
				return map[string]string{"NO_COLOR": tc.noColor, "TERM": tc.term}[key], true
			})
			if err := p.Fields("App", [][2]string{{"Status", "running"}}); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(out.String(), "\x1b[") != tc.wantColor {
				t.Fatalf("color policy: %q", out.String())
			}
			out.Reset()
			p.Report(errors.New("denied"), true)
			if !json.Valid(out.Bytes()) || strings.Contains(out.String(), "\x1b") {
				t.Fatalf("styled JSON: %q", out.String())
			}
		})
	}
}

func TestTableAlignmentAndTerminalSafety(t *testing.T) {
	var out bytes.Buffer
	p := &Printer{w: &out, width: 100}
	rows := [][]string{{"日本語", "running", "one"}, {"api", "failed", "two"}, {"evil\x1b[31m\t\n", "queued", "three"}}
	if err := p.Table([]string{"NAME", "STATUS", "ID"}, rows, "No apps."); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 4 || strings.Contains(out.String(), "\x1b") || strings.Contains(out.String(), "\t") {
		t.Fatalf("unsafe table: %q", out.String())
	}
	wantColumn := runewidth.StringWidth(lines[0][:strings.Index(lines[0], "STATUS")])
	for i, row := range rows {
		column := runewidth.StringWidth(lines[i+1][:strings.Index(lines[i+1], row[1])])
		if column != wantColumn {
			t.Fatalf("status column %d, want %d: %q", column, wantColumn, out.String())
		}
	}
	if !strings.Contains(out.String(), `evil\u001b[31m\u0009\u000a`) {
		t.Fatal("untrusted controls were not escaped")
	}
}

func TestNarrowTablePreservesIdentifiers(t *testing.T) {
	var out bytes.Buffer
	p := &Printer{w: &out, width: 60}
	id := "11111111-1111-4111-8111-111111111111"
	if err := p.Table([]string{"NAME", "STATUS", "APP ID"}, [][]string{{"a-very-long-app-name", "running", id}}, "No apps."); err != nil {
		t.Fatal(err)
	}
	if strings.Count(out.String(), "\n") != 3 || !strings.Contains(out.String(), id) {
		t.Fatalf("narrow layout lost fields: %q", out.String())
	}
}

type failedWriter struct{}

func (failedWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestFormattingPropagatesWriteErrors(t *testing.T) {
	p := &Printer{w: failedWriter{}}
	for _, err := range []error{
		p.Fields("App", [][2]string{{"Status", "running"}}),
		p.Table([]string{"NAME"}, [][]string{{"example"}}, "No apps."),
		p.Table([]string{"NAME"}, nil, "No apps."),
		p.Message("Done."),
	} {
		if !errors.Is(err, io.ErrClosedPipe) {
			t.Fatalf("write failure lost: %v", err)
		}
	}
}
