package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/Songxwn/Rack-auto/internal/agent"
)

var Version = "dev"

func main() {
	url := flag.String("url", os.Getenv("RACKAUTO_URL"), "control-plane URL")
	token := flag.String("token", os.Getenv("RACKAUTO_TOKEN"), "API token")
	mac := flag.String("mac", os.Getenv("RACKAUTO_MAC"), "NIC MAC (auto-detect if empty)")
	flag.Parse()
	if len(flag.Args()) > 0 && flag.Arg(0) == "version" {
		fmt.Println("rackauto-agent", Version)
		return
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := agent.Run(ctx, *url, *token, *mac, Version); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
