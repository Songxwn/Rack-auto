package agent

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/Songxwn/Rack-auto/internal/model"
)

type Client struct {
	URL     string
	Token   string
	MAC     string
	HTTP    *http.Client
	Machine string
	Version string
}

func New(url, token, mac, version string) *Client {
	return &Client{
		URL:     strings.TrimRight(url, "/"),
		Token:   token,
		MAC:     mac,
		Version: version,
		HTTP:    &http.Client{Timeout: 10 * time.Minute},
	}
}

func (c *Client) do(method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.URL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		req.Header.Set("X-API-Token", c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if resp.StatusCode >= 400 {
		return fmt.Errorf("%s %s: %s %s", method, path, resp.Status, strings.TrimSpace(string(b)))
	}
	if out == nil || len(b) == 0 {
		return nil
	}
	return json.Unmarshal(b, out)
}

func (c *Client) Register(inv *model.Inventory) error {
	ip := PrimaryIP()
	var res struct {
		MachineID string `json:"machine_id"`
	}
	err := c.do(http.MethodPost, "/api/v1/agent/register", model.AgentRegister{
		MAC: c.MAC, IP: ip, Hostname: inv.Hostname, AgentVersion: c.Version, Inventory: inv,
	}, &res)
	if err != nil {
		return err
	}
	c.Machine = res.MachineID
	return nil
}

func (c *Client) PollJob() (*model.AgentJob, error) {
	q := "/api/v1/agent/job?mac=" + c.MAC
	if c.Machine != "" {
		q += "&machine_id=" + c.Machine
	}
	var res struct {
		Job *model.AgentJob `json:"job"`
	}
	if err := c.do(http.MethodGet, q, nil, &res); err != nil {
		return nil, err
	}
	return res.Job, nil
}

func (c *Client) Log(jobID, line string) {
	_ = c.do(http.MethodPost, "/api/v1/agent/jobs/"+jobID+"/log", map[string]string{"line": line}, nil)
}

func (c *Client) Progress(jobID string, n int, msg string) {
	_ = c.do(http.MethodPost, "/api/v1/agent/jobs/"+jobID+"/progress", map[string]any{"progress": n, "message": msg}, nil)
}

func (c *Client) Complete(jobID string, ok bool, msg string, result any) error {
	return c.do(http.MethodPost, "/api/v1/agent/jobs/"+jobID+"/complete", map[string]any{"ok": ok, "message": msg, "result": result}, nil)
}

func PrimaryMAC() string {
	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}
		if len(iface.HardwareAddr) == 6 {
			return strings.ToLower(iface.HardwareAddr.String())
		}
	}
	return ""
}

func PrimaryIP() string {
	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok || ipnet.IP.To4() == nil || ipnet.IP.IsLoopback() {
				continue
			}
			return ipnet.IP.String()
		}
	}
	return ""
}

func CollectInventory() *model.Inventory {
	inv := &model.Inventory{
		Hostname: hostname(),
		Arch:     runtime.GOARCH,
		CPUs:     runtime.NumCPU(),
		Kernel:   uname(),
		Firmware: firmware(),
		MemoryMB: memMB(),
		CPUModel: cpuModel(),
		Disks:    listDisks(),
		NICs:     listNICs(),
		UptimeSec: uptime(),
	}
	return inv
}

func hostname() string {
	h, _ := os.Hostname()
	return h
}

func firmware() string {
	if _, err := os.Stat("/sys/firmware/efi"); err == nil {
		return model.FirmwareUEFI
	}
	return model.FirmwareBIOS
}

func uname() string {
	b, err := os.ReadFile("/proc/version")
	if err != nil {
		return runtime.GOOS + "/" + runtime.GOARCH
	}
	return strings.TrimSpace(string(b))
}

func memMB() int {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if strings.HasPrefix(sc.Text(), "MemTotal:") {
			fields := strings.Fields(sc.Text())
			if len(fields) >= 2 {
				kb, _ := strconv.Atoi(fields[1])
				return kb / 1024
			}
		}
	}
	return 0
}

func cpuModel() string {
	b, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "model name") {
			_, v, ok := strings.Cut(line, ":")
			if ok {
				return strings.TrimSpace(v)
			}
		}
	}
	return ""
}

func uptime() int64 {
	b, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	var f float64
	fmt.Sscanf(string(b), "%f", &f)
	return int64(f)
}

func listNICs() []model.NIC {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []model.NIC
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		n := model.NIC{Name: iface.Name, MAC: strings.ToLower(iface.HardwareAddr.String()), MTU: iface.MTU, Up: iface.Flags&net.FlagUp != 0}
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			n.IPs = append(n.IPs, a.String())
		}
		if p := filepath.Join("/sys/class/net", iface.Name, "speed"); true {
			if b, err := os.ReadFile(p); err == nil {
				n.Speed = strings.TrimSpace(string(b)) + "Mb/s"
			}
		}
		out = append(out, n)
	}
	return out
}

func listDisks() []model.Disk {
	entries, err := os.ReadDir("/sys/block")
	if err != nil {
		return nil
	}
	var out []model.Disk
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") || strings.HasPrefix(name, "sr") || strings.HasPrefix(name, "fd") {
			continue
		}
		sizeB := readSysInt("/sys/block/"+name+"/size") * 512
		if sizeB <= 0 {
			continue
		}
		rot := readSysInt("/sys/block/"+name+"/queue/rotational") == 1
		modelName, _ := os.ReadFile("/sys/block/" + name + "/device/model")
		serial, _ := os.ReadFile("/sys/block/" + name + "/device/serial")
		out = append(out, model.Disk{
			Name: name, Path: "/dev/" + name, SizeB: sizeB, Rot: rot,
			Model:  strings.TrimSpace(string(modelName)),
			Serial: strings.TrimSpace(string(serial)),
		})
	}
	return out
}

func readSysInt(path string) int64 {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	n, _ := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	return n
}

func DecodeJSON[T any](v any) (T, error) {
	var zero T
	b, err := json.Marshal(v)
	if err != nil {
		return zero, err
	}
	err = json.Unmarshal(b, &zero)
	return zero, err
}
