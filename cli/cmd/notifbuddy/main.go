package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"xolo/cli/internal/cmd"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(cmd.Execute(ctx))
}
