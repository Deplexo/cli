package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/Deplexo/cli/internal/command"
)

var version = "dev"
var commit = "unknown"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := command.Execute(ctx, command.Options{Version: version, Commit: commit}, os.Args[1:])
	stop()
	os.Exit(code)
}
