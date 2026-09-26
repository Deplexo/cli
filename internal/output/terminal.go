package output

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mattn/go-colorable"
	"github.com/mattn/go-runewidth"
	"golang.org/x/term"
)

// Printer formats human output. JSON and completion scripts bypass it.
type Printer struct {
	w     io.Writer
	color bool
	width int
}

func NewPrinter(w io.Writer, mode string, lookup func(string) (string, bool)) *Printer {
	if lookup == nil {
		lookup = os.LookupEnv
	}
	p := &Printer{w: w}
	file, isFile := w.(*os.File)
	terminal := isFile && term.IsTerminal(int(file.Fd()))
	noColor, _ := lookup("NO_COLOR")
	terminalType, _ := lookup("TERM")
	p.color = noColor == "" && terminalType != "dumb" && (mode == "always" || (mode == "auto" && terminal))
	if terminal {
		p.width, _, _ = term.GetSize(int(file.Fd()))
		if p.color {
			p.w = colorable.NewColorable(file)
		}
	}
	return p
}

func (p *Printer) style(text, code string) string {
	if p.color {
		return "\x1b[" + code + "m" + text + "\x1b[0m"
	}
	return text
}

func (p *Printer) status(value string) string {
	code := ""
	switch strings.ToLower(value) {
	case "running", "success", "succeeded", "ready", "active":
		code = "32"
	case "pending", "queued", "building", "deploying", "starting", "stopping":
		code = "33"
	case "failed", "error", "crashed", "violated":
		code = "31"
	case "stopped", "cancelled", "canceled", "deleted":
		code = "90"
	}
	if code != "" {
		return p.style(value, code)
	}
	return value
}

func (p *Printer) Message(text string) error {
	_, err := fmt.Fprintln(p.w, p.style(Safe(text), "32"))
	return err
}

func (p *Printer) Error(message string) {
	_, _ = fmt.Fprintln(p.w, p.style("Error:", "1;31")+" "+Safe(message))
}

// Fields keeps identifiers intact, including when tables become too wide.
func (p *Printer) Fields(title string, fields [][2]string) error {
	if title != "" {
		if _, err := fmt.Fprintln(p.w, p.style(Safe(title), "1;36")); err != nil {
			return err
		}
	}
	width := 0
	for _, field := range fields {
		width = max(width, runewidth.StringWidth(Safe(field[0])))
	}
	for _, field := range fields {
		label, value := Safe(field[0]), Safe(field[1])
		if value == "" {
			value = "-"
		}
		prefix := "  " + label + strings.Repeat(" ", width-runewidth.StringWidth(label)+2)
		if _, err := fmt.Fprintln(p.w, p.style(prefix, "1")+p.status(value)); err != nil {
			return err
		}
	}
	return nil
}

func (p *Printer) Table(headers []string, rows [][]string, empty string) error {
	if len(rows) == 0 {
		_, err := fmt.Fprintln(p.w, Safe(empty))
		return err
	}
	widths := make([]int, len(headers))
	for i, header := range headers {
		widths[i] = runewidth.StringWidth(Safe(header))
	}
	for _, row := range rows {
		for i := range headers {
			if i < len(row) {
				widths[i] = max(widths[i], runewidth.StringWidth(Safe(row[i])))
			}
		}
	}
	total := 2 * (len(headers) - 1)
	for _, width := range widths {
		total += width
	}
	if p.width > 0 && total > p.width {
		for n, row := range rows {
			if n > 0 {
				if _, err := fmt.Fprintln(p.w); err != nil {
					return err
				}
			}
			fields := make([][2]string, len(headers))
			for i, header := range headers {
				fields[i][0] = header
				if i < len(row) {
					fields[i][1] = row[i]
				}
			}
			if err := p.Fields("", fields); err != nil {
				return err
			}
		}
		return nil
	}
	for n, row := range append([][]string{headers}, rows...) {
		var line strings.Builder
		for i := range headers {
			value := ""
			if i < len(row) {
				value = Safe(row[i])
			}
			if n == 0 {
				line.WriteString(p.style(value, "1;36"))
			} else {
				line.WriteString(p.status(value))
			}
			if i < len(headers)-1 {
				line.WriteString(strings.Repeat(" ", widths[i]-runewidth.StringWidth(value)+2))
			}
		}
		if _, err := fmt.Fprintln(p.w, line.String()); err != nil {
			return err
		}
	}
	return nil
}

func (p *Printer) Help(text string) error {
	for _, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		line = Safe(line)
		if strings.HasSuffix(line, ":") && !strings.HasPrefix(line, " ") {
			line = p.style(line, "1;36")
		}
		if _, err := fmt.Fprintln(p.w, line); err != nil {
			return err
		}
	}
	return nil
}
