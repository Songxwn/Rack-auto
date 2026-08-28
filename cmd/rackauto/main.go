package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Songxwn/Rack-auto/internal/bootstrap"
	"github.com/Songxwn/Rack-auto/internal/config"
	"github.com/Songxwn/Rack-auto/internal/netboot"
	"github.com/Songxwn/Rack-auto/internal/server"
	"github.com/Songxwn/Rack-auto/internal/store"
	"github.com/Songxwn/Rack-auto/internal/winpecurl"
)

var Version = "dev"

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("rackauto ")
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "bootstrap":
			os.Exit(runBootstrap(os.Args[2:]))
		case "version", "-version", "--version":
			fmt.Println("rackauto", Version)
			return
		case "help", "-h", "--help":
			usage()
			return
		}
	}

	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	cfgPath := fs.String("config", env("RACKAUTO_CONFIG", "configs/rackauto.yaml"), "config file path")
	listen := fs.String("listen", "", "HTTP listen address")
	publicURL := fs.String("public-url", "", "public URL")
	dataDir := fs.String("data-dir", "", "data directory")
	_ = fs.Parse(filterServeArgs(os.Args[1:]))

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatal(err)
	}
	if *listen != "" {
		cfg.Listen = *listen
	}
	if *publicURL != "" {
		cfg.PublicURL = *publicURL
	}
	if *dataDir != "" {
		cfg.DataDir = *dataDir
	}
	if err := cfg.EnsureDirs(); err != nil {
		log.Fatal(err)
	}
	if err := bootstrap.CheckRAMOS(cfg); err != nil {
		log.Printf("WARNING: %v", err)
	}
	if err := bootstrap.InstallIPXE(cfg.TFTPDir()); err != nil {
		log.Printf("install bundled iPXE: %v", err)
	}
	if err := winpecurl.Install(cfg); err != nil {
		log.Printf("WinPE curl.exe: %v", err)
	} else if p := winpecurl.Path(cfg); p != "" {
		log.Printf("WinPE curl.exe %s", p)
	}
	st, err := store.Open(cfg.DBPath())
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()
	if cfg.APIToken != "" {
		_ = st.SetSetting("api_token", cfg.APIToken)
	}
	if cfg.PublicURL != "" {
		if st.Setting("public_url", "") == "" {
			_ = st.SetSetting("public_url", cfg.PublicURL)
		}
	}

	nb := netboot.New(cfg, st)
	if err := nb.StartTFTP(); err != nil {
		log.Printf("TFTP start failed (needs privileged port 69): %v", err)
	} else {
		log.Printf("TFTP %s dir %s", cfg.TFTPListen, cfg.TFTPDir())
	}
	if err := nb.StartDHCP(); err != nil {
		log.Printf("DHCP start failed: %v", err)
	} else if nb.CurrentDHCP().Enabled {
		st := nb.DHCPStatus()
		log.Printf("DHCP iface %s listen %s", st.Interface, st.Listen)
	}

	srv := server.New(cfg, st, nb)
	srv.Version = Version
	httpSrv := &http.Server{Addr: cfg.Listen, Handler: srv.Handler(), ReadHeaderTimeout: 15 * time.Second}
	go func() {
		log.Printf("console http://%s  version %s", displayAddr(cfg.Listen), Version)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	nb.CloseDHCP()
	_ = httpSrv.Shutdown(ctx)
}

func runBootstrap(args []string) int {
	fs := flag.NewFlagSet("bootstrap", flag.ExitOnError)
	cfgPath := fs.String("config", env("RACKAUTO_CONFIG", "configs/rackauto.yaml"), "config file")
	dataDir := fs.String("data-dir", "", "data directory")
	offline := fs.Bool("offline", false, "offline: bundled iPXE and cached Ubuntu RAMOS only")
	_ = fs.Parse(args)
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Println(err)
		return 1
	}
	if *dataDir != "" {
		cfg.DataDir = *dataDir
	}
	if err := bootstrap.Run(cfg, ".", *offline); err != nil {
		log.Println(err)
		return 1
	}
	return 0
}

func usage() {
	fmt.Fprintf(os.Stderr, `Rack-auto %s - bare-metal iPXE provisioner

Usage:
  rackauto serve [flags]      start control plane (HTTP / TFTP / optional DHCP)
  rackauto bootstrap [flags]  install local iPXE, cache Ubuntu RAMOS, build agent
  rackauto version

Flags:
  -config string       config file (default configs/rackauto.yaml)
  -listen string       HTTP listen address
  -public-url string   URL machines can reach
  -data-dir string     data directory
  -offline             bootstrap without Internet (need cached Ubuntu live-server)
`, Version)
}

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func filterServeArgs(args []string) []string {
	if len(args) > 0 && args[0] == "serve" {
		return args[1:]
	}
	return args
}

func displayAddr(listen string) string {
	if len(listen) > 0 && listen[0] == ':' {
		return "127.0.0.1" + listen
	}
	return listen
}
