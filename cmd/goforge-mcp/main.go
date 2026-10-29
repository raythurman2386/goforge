// Command goforge-mcp is the GoForge MCP server. It speaks MCP over stdio
// (run it over ssh) and calls the GoForge /api/v1 JSON API with a scoped
// bearer token. See docs/guides/mcp.md.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/raythurman2386/goforge/internal/mcpserver"
)

func main() {
	os.Exit(run())
}

func run() int {
	if len(os.Args) > 1 && (os.Args[1] == "-version" || os.Args[1] == "--version") {
		fmt.Println("goforge-mcp", mcpserver.Version)
		return 0
	}
	cfg, err := mcpserver.LoadConfig(os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, "goforge-mcp:", err)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := mcpserver.New(cfg, nil).Run(ctx); err != nil && ctx.Err() == nil {
		fmt.Fprintln(os.Stderr, "goforge-mcp:", err)
		return 1
	}
	return 0
}
