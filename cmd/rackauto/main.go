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
	cfgPath := fs.String("config", env("RACKAUTO_CONFIG", "configs/rackauto.yaml"), "配置文件路径")
	listen := fs.String("listen", "", "HTTP 监听地址")
	publicURL := fs.String("public-url", "", "对外可达 URL")
	dataDir := fs.String("data-dir", "", "数据目录")
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
		log.Printf("TFTP 启动失败（需要特权端口 69）: %v", err)
	} else {
		log.Printf("TFTP %s 目录 %s", cfg.TFTPListen, cfg.TFTPDir())
	}
	if err := nb.StartDHCP(); err != nil {
		log.Printf("DHCP 启动失败: %v", err)
	} else if nb.CurrentDHCP().Enabled {
		st := nb.DHCPStatus()
		log.Printf("DHCP 网卡 %s 监听 %s", st.Interface, st.Listen)
	}

	srv := server.New(cfg, st, nb)
	httpSrv := &http.Server{Addr: cfg.Listen, Handler: srv.Handler(), ReadHeaderTimeout: 15 * time.Second}
	go func() {
		log.Printf("控制台 http://%s  版本 %s", displayAddr(cfg.Listen), Version)
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
	cfgPath := fs.String("config", env("RACKAUTO_CONFIG", "configs/rackauto.yaml"), "配置文件")
	dataDir := fs.String("data-dir", "", "数据目录")
	_ = fs.Parse(args)
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Println(err)
		return 1
	}
	if *dataDir != "" {
		cfg.DataDir = *dataDir
	}
	if err := bootstrap.Run(cfg, "."); err != nil {
		log.Println(err)
		return 1
	}
	return 0
}

func usage() {
	fmt.Fprintf(os.Stderr, `Rack-auto %s — 裸金属 iPXE 装机平台

用法:
  rackauto serve [选项]       启动控制面（HTTP / TFTP / 可选 DHCP）
  rackauto bootstrap [选项]   下载 iPXE + Alpine RAMOS 并交叉编译 Agent
  rackauto version

选项:
  -config string       配置文件 (默认 configs/rackauto.yaml)
  -listen string       HTTP 监听
  -public-url string   机器可达的控制面 URL
  -data-dir string     数据目录
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
