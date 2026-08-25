package agent

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Songxwn/Rack-auto/internal/model"
)

func Run(ctx context.Context, url, token, mac, version string) error {
	if url == "" {
		url = fromCmdline("rackauto_url")
	}
	if token == "" {
		token = fromCmdline("rackauto_token")
	}
	if mac == "" {
		mac = fromCmdline("rackauto_mac")
	}
	if mac == "" {
		mac = PrimaryMAC()
	}
	if url == "" {
		return fmt.Errorf("未指定控制面 URL（--url 或内核参数 rackauto_url）")
	}
	c := New(url, token, mac, version)
	inv := CollectInventory()
	fmt.Printf("RAMOS agent %s mac=%s url=%s\n", version, mac, url)
	for {
		if err := c.Register(inv); err != nil {
			fmt.Println("register:", err)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
				continue
			}
		}
		break
	}
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
			job, err := c.PollJob()
			if err != nil {
				fmt.Println("poll:", err)
				inv = CollectInventory()
				_ = c.Register(inv)
				continue
			}
			if job == nil {
				continue
			}
			fmt.Println("got job", job.ID, job.Type)
			switch job.Type {
			case model.JobInstall:
				err = c.RunInstall(ctx, job)
				msg := "装机完成"
				if err != nil {
					msg = err.Error()
				}
				_ = c.Complete(job.ID, err == nil, msg, nil)
			case model.JobStress:
				res, err := c.RunStress(ctx, job)
				msg := "压测完成"
				if err != nil {
					msg = err.Error()
				}
				_ = c.Complete(job.ID, err == nil, msg, res)
			default:
				_ = c.Complete(job.ID, false, "未知任务类型 "+job.Type, nil)
			}
			inv = CollectInventory()
			_ = c.Register(inv)
		}
	}
}

func fromCmdline(key string) string {
	b, err := os.ReadFile("/proc/cmdline")
	if err != nil {
		return ""
	}
	prefix := key + "="
	for _, f := range strings.Fields(string(b)) {
		if strings.HasPrefix(f, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(f, prefix))
		}
	}
	return os.Getenv(strings.ToUpper(key))
}
