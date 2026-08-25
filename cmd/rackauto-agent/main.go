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
	url := flag.String("url", os.Getenv("RACKAUTO_URL"), "控制面 URL")
	token := flag.String("token", os.Getenv("RACKAUTO_TOKEN"), "API Token")
	mac := flag.String("mac", os.Getenv("RACKAUTO_MAC"), "网卡 MAC（默认自动探测）")
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
