//go:build ignore

// Generate the installer's release pointer from published GitHub releases.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/Deplexo/cli/internal/update"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tag, err := update.New(nil).Latest(ctx)
	if err == nil {
		err = os.WriteFile("site/latest-version", []byte(tag+"\n"), 0644)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
