package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/Deplexo/cli/internal/command"
	buildversion "github.com/Deplexo/cli/internal/version"
)

var version = "dev"
var commit = "unknown"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	v, revision := buildversion.Current(version, commit)
	code := command.Execute(ctx, command.Options{Version: v, Commit: revision}, os.Args[1:])
	stop()
	os.Exit(code)
}
