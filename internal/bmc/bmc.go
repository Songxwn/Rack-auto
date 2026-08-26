package bmc

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"

	ipmiclient "github.com/bougou/go-ipmi/pkg/client"
	"github.com/bougou/go-ipmi/pkg/command/chassis"
	"github.com/bougou/go-ipmi/pkg/types"

	"github.com/Songxwn/Rack-auto/internal/model"
)

type Controller interface {
	PowerStatus(ctx context.Context) (string, error)
	Power(ctx context.Context, action string) error
	SetBoot(ctx context.Context, device, firmware string, persistent bool) error
}

func Open(m model.Machine) (Controller, error) {
	switch strings.ToLower(m.BMCType) {
	case model.BMCRedfish:
		if m.BMCAddress == "" {
			return nil, fmt.Errorf("缺少 Redfish 地址")
		}
		return &Redfish{Machine: m}, nil
	case model.BMCIPMI, "":
		if m.BMCAddress == "" {
			return nil, fmt.Errorf("缺少 IPMI 地址")
		}
		port := m.BMCPort
		if port == 0 {
			port = 623
		}
		return &IPMI{
			Host: m.BMCAddress,
			Port: port,
			User: m.BMCUsername,
			Pass: m.BMCPassword,
		}, nil
	default:
		return nil, fmt.Errorf("不支持的 BMC 类型: %s", m.BMCType)
	}
}

type IPMI struct {
	Host string
	Port int
	User string
	Pass string
}

func (i *IPMI) withClient(ctx context.Context, fn func(*ipmiclient.Client) error) error {
	c, err := ipmiclient.NewClient(i.Host, i.Port, i.User, i.Pass)
	if err != nil {
		return err
	}
	if err := c.Connect(ctx); err != nil {
		return fmt.Errorf("IPMI 连接失败: %w", err)
	}
	defer c.Close(ctx)
	return fn(c)
}

func (i *IPMI) PowerStatus(ctx context.Context) (string, error) {
	var status string
	err := i.withClient(ctx, func(c *ipmiclient.Client) error {
		res, err := c.GetChassisStatus(ctx)
		if err != nil {
			return err
		}
		if res.PowerIsOn {
			status = "on"
		} else {
			status = "off"
		}
		return nil
	})
	if err != nil {
		out, e2 := i.ipmitool(ctx, "chassis", "power", "status")
		if e2 != nil {
			return "", err
		}
		if strings.Contains(strings.ToLower(out), "on") {
			return "on", nil
		}
		return "off", nil
	}
	return status, nil
}

func (i *IPMI) Power(ctx context.Context, action string) error {
	var ctl chassis.ChassisControl
	switch strings.ToLower(action) {
	case "on":
		ctl = chassis.ChassisControlPowerUp
	case "off":
		ctl = chassis.ChassisControlPowerDown
	case "cycle":
		ctl = chassis.ChassisControlPowerCycle
	case "reset":
		ctl = chassis.ChassisControlHardReset
	case "soft":
		ctl = chassis.ChassisControlSoftShutdown
	default:
		return fmt.Errorf("未知电源操作: %s", action)
	}
	err := i.withClient(ctx, func(c *ipmiclient.Client) error {
		_, e := c.ChassisControl(ctx, ctl)
		return e
	})
	if err != nil {
		arg := map[string]string{"on": "on", "off": "off", "cycle": "cycle", "reset": "reset", "soft": "soft"}[strings.ToLower(action)]
		_, e2 := i.ipmitool(ctx, "chassis", "power", arg)
		if e2 != nil {
			return err
		}
	}
	return nil
}

func (i *IPMI) SetBoot(ctx context.Context, device, firmware string, persistent bool) error {
	sel := types.BootDeviceSelectorForcePXE
	switch strings.ToLower(device) {
	case "pxe", "network":
		sel = types.BootDeviceSelectorForcePXE
	case "disk", "hdd":
		sel = types.BootDeviceSelectorForceHardDrive
	case "cd", "cdrom", "dvd":
		sel = types.BootDeviceSelectorForceCDROM
	case "bios", "setup":
		sel = types.BootDeviceSelectorForceBIOSSetup
	default:
		return fmt.Errorf("未知引导设备: %s", device)
	}
	bootType := types.BIOSBootTypeLegacy
	if strings.EqualFold(firmware, model.FirmwareUEFI) || strings.EqualFold(firmware, "efi") {
		bootType = types.BIOSBootTypeEFI
	}
	err := i.withClient(ctx, func(c *ipmiclient.Client) error {
		return c.SetBootDevice(ctx, sel, bootType, persistent)
	})
	if err != nil {
		dev := map[string]string{"pxe": "pxe", "network": "pxe", "disk": "disk", "hdd": "disk", "cd": "cdrom", "cdrom": "cdrom", "bios": "bios"}[strings.ToLower(device)]
		args := []string{"chassis", "bootdev", dev}
		if strings.EqualFold(firmware, model.FirmwareUEFI) {
			args = append(args, "options=efiboot")
		}
		if persistent {
			args = append(args, "options=persistent")
		}
		if _, e2 := i.ipmitool(ctx, args...); e2 != nil {
			return err
		}
	}
	return nil
}

func (i *IPMI) ipmitool(ctx context.Context, args ...string) (string, error) {
	if _, err := exec.LookPath("ipmitool"); err != nil {
		return "", err
	}
	all := []string{"-I", "lanplus", "-H", i.Host, "-p", strconv.Itoa(i.Port), "-U", i.User, "-P", i.Pass}
	all = append(all, args...)
	cmd := exec.CommandContext(ctx, "ipmitool", all...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

type Redfish struct {
	Machine model.Machine
	client  *http.Client
	token   string
	session string
	base    string
}

func (r *Redfish) http() *http.Client {
	if r.client == nil {
		r.client = &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: r.Machine.BMCInsecure}, //nolint:gosec
			},
		}
	}
	return r.client
}

func (r *Redfish) endpoint() (string, error) {
	if r.base != "" {
		return r.base, nil
	}
	raw := strings.TrimSpace(r.Machine.BMCAddress)
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = "/redfish/v1"
	}
	r.base = strings.TrimRight(u.String(), "/")
	return r.base, nil
}

func (r *Redfish) do(ctx context.Context, method, path string, body any) (*http.Response, []byte, error) {
	base, err := r.endpoint()
	if err != nil {
		return nil, nil, err
	}
	full := path
	if strings.HasPrefix(path, "/") {
		u, _ := url.Parse(base)
		full = u.Scheme + "://" + u.Host + path
	} else if !strings.Contains(path, "://") {
		full = base + "/" + strings.TrimPrefix(path, "/")
	}
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, nil, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, full, rdr)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("OData-Version", "4.0")
	req.Header.Set("If-Match", "*")
	if r.token != "" {
		req.Header.Set("X-Auth-Token", r.token)
	} else {
		req.SetBasicAuth(r.Machine.BMCUsername, r.Machine.BMCPassword)
	}
	resp, err := r.http().Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode >= 400 {
		return resp, b, fmt.Errorf("redfish %s %s: %s %s", method, full, resp.Status, truncate(string(b), 400))
	}
	return resp, b, nil
}

func (r *Redfish) login(ctx context.Context) error {
	base, err := r.endpoint()
	if err != nil {
		return err
	}
	u, _ := url.Parse(base)
	sessionURL := u.Scheme + "://" + u.Host + "/redfish/v1/SessionService/Sessions"
	payload := map[string]string{"UserName": r.Machine.BMCUsername, "Password": r.Machine.BMCPassword}
	resp, _, err := r.do(ctx, http.MethodPost, sessionURL, payload)
	if err != nil {
		return nil
	}
	if t := resp.Header.Get("X-Auth-Token"); t != "" {
		r.token = t
	}
	if loc := resp.Header.Get("Location"); loc != "" {
		r.session = loc
	}
	return nil
}

func (r *Redfish) systemPath(ctx context.Context) (string, error) {
	_ = r.login(ctx)
	_, body, err := r.do(ctx, http.MethodGet, "/redfish/v1/Systems", nil)
	if err != nil {
		return "", err
	}
	var col struct {
		Members []struct {
			Path string `json:"@odata.id"`
		} `json:"Members"`
	}
	if err := json.Unmarshal(body, &col); err != nil {
		return "", err
	}
	if len(col.Members) == 0 {
		return "", fmt.Errorf("Redfish 未发现 ComputerSystem")
	}
	return col.Members[0].Path, nil
}

func (r *Redfish) PowerStatus(ctx context.Context) (string, error) {
	path, err := r.systemPath(ctx)
	if err != nil {
		return "", err
	}
	_, body, err := r.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return "", err
	}
	var sys struct {
		PowerState string `json:"PowerState"`
	}
	_ = json.Unmarshal(body, &sys)
	st := strings.ToLower(sys.PowerState)
	if st == "" {
		st = "unknown"
	}
	return st, nil
}

func (r *Redfish) Power(ctx context.Context, action string) error {
	path, err := r.systemPath(ctx)
	if err != nil {
		return err
	}
	reset := map[string]string{
		"on": "On", "off": "ForceOff", "cycle": "PowerCycle",
		"reset": "ForceRestart", "soft": "GracefulShutdown",
	}[strings.ToLower(action)]
	if reset == "" {
		return fmt.Errorf("未知电源操作: %s", action)
	}
	_, _, err = r.do(ctx, http.MethodPost, strings.TrimRight(path, "/")+"/Actions/ComputerSystem.Reset", map[string]string{"ResetType": reset})
	return err
}

func (r *Redfish) SetBoot(ctx context.Context, device, firmware string, persistent bool) error {
	path, err := r.systemPath(ctx)
	if err != nil {
		return err
	}
	target := map[string]string{
		"pxe": "Pxe", "network": "Pxe", "disk": "Hdd", "hdd": "Hdd",
		"cd": "Cd", "cdrom": "Cd", "bios": "BiosSetup", "setup": "BiosSetup",
	}[strings.ToLower(device)]
	if target == "" {
		return fmt.Errorf("未知引导设备: %s", device)
	}
	enabled := "Once"
	if persistent {
		enabled = "Continuous"
	}
	boot := map[string]any{
		"BootSourceOverrideEnabled": enabled,
		"BootSourceOverrideTarget":  target,
	}
	if strings.EqualFold(firmware, model.FirmwareUEFI) || strings.EqualFold(firmware, "efi") {
		boot["BootSourceOverrideMode"] = "UEFI"
	} else if strings.EqualFold(firmware, model.FirmwareBIOS) || strings.EqualFold(firmware, "legacy") {
		boot["BootSourceOverrideMode"] = "Legacy"
	}
	_, _, err = r.do(ctx, http.MethodPatch, path, map[string]any{"Boot": boot})
	return err
}

func (r *Redfish) ReadInventory(ctx context.Context) (*model.Inventory, error) {
	path, err := r.systemPath(ctx)
	if err != nil {
		return nil, err
	}
	_, body, err := r.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	inv := parseRedfishSystem(body)
	if inv.Serial == "" {
		if ch, err := r.chassisPath(ctx); err == nil && ch != "" {
			if _, cb, err := r.do(ctx, http.MethodGet, ch, nil); err == nil {
				fillRedfishChassis(inv, cb)
			}
		}
	}
	if inv.HasIdentity() || inv.BIOSVersion != "" {
		inv.DetectSource = "redfish"
	}
	return inv, nil
}

func (r *Redfish) chassisPath(ctx context.Context) (string, error) {
	_, body, err := r.do(ctx, http.MethodGet, "/redfish/v1/Chassis", nil)
	if err != nil {
		return "", err
	}
	var col struct {
		Members []struct {
			Path string `json:"@odata.id"`
		} `json:"Members"`
	}
	if err := json.Unmarshal(body, &col); err != nil {
		return "", err
	}
	if len(col.Members) == 0 {
		return "", fmt.Errorf("Redfish 未发现 Chassis")
	}
	return col.Members[0].Path, nil
}

func parseRedfishSystem(body []byte) *model.Inventory {
	var sys struct {
		Manufacturer string `json:"Manufacturer"`
		Model        string `json:"Model"`
		SKU          string `json:"SKU"`
		SerialNumber string `json:"SerialNumber"`
		UUID         string `json:"UUID"`
		PartNumber   string `json:"PartNumber"`
		BiosVersion  string `json:"BiosVersion"`
		HostName     string `json:"HostName"`
		AssetTag     string `json:"AssetTag"`
	}
	_ = json.Unmarshal(body, &sys)
	inv := &model.Inventory{
		Hostname:    strings.TrimSpace(sys.HostName),
		Vendor:      strings.TrimSpace(sys.Manufacturer),
		Product:     strings.TrimSpace(sys.Model),
		Serial:      strings.TrimSpace(sys.SerialNumber),
		SKU:         strings.TrimSpace(sys.SKU),
		UUID:        strings.TrimSpace(sys.UUID),
		AssetTag:    strings.TrimSpace(sys.AssetTag),
		BIOSVersion: strings.TrimSpace(sys.BiosVersion),
	}
	if inv.SKU == "" {
		inv.SKU = strings.TrimSpace(sys.PartNumber)
	}
	return inv
}

func fillRedfishChassis(inv *model.Inventory, body []byte) {
	if inv == nil {
		return
	}
	var ch struct {
		SerialNumber string `json:"SerialNumber"`
		SKU          string `json:"SKU"`
		AssetTag     string `json:"AssetTag"`
		Manufacturer string `json:"Manufacturer"`
		Model        string `json:"Model"`
	}
	_ = json.Unmarshal(body, &ch)
	if inv.Serial == "" {
		inv.Serial = strings.TrimSpace(ch.SerialNumber)
	}
	if inv.SKU == "" {
		inv.SKU = strings.TrimSpace(ch.SKU)
	}
	if inv.AssetTag == "" {
		inv.AssetTag = strings.TrimSpace(ch.AssetTag)
	}
	if inv.Vendor == "" {
		inv.Vendor = strings.TrimSpace(ch.Manufacturer)
	}
	if inv.Product == "" {
		inv.Product = strings.TrimSpace(ch.Model)
	}
}

func MergeIdentity(dst, src *model.Inventory) *model.Inventory {
	if dst == nil {
		dst = &model.Inventory{}
	}
	if src == nil {
		return dst
	}
	set := func(cur *string, v string) {
		if strings.TrimSpace(v) != "" {
			*cur = strings.TrimSpace(v)
		}
	}
	set(&dst.Vendor, src.Vendor)
	set(&dst.Product, src.Product)
	set(&dst.ProductVersion, src.ProductVersion)
	set(&dst.Serial, src.Serial)
	set(&dst.SKU, src.SKU)
	set(&dst.UUID, src.UUID)
	set(&dst.Family, src.Family)
	set(&dst.BoardVendor, src.BoardVendor)
	set(&dst.BoardName, src.BoardName)
	set(&dst.BoardSerial, src.BoardSerial)
	set(&dst.AssetTag, src.AssetTag)
	set(&dst.BIOSVendor, src.BIOSVendor)
	set(&dst.BIOSVersion, src.BIOSVersion)
	set(&dst.BIOSDate, src.BIOSDate)
	if src.DetectSource != "" {
		dst.DetectSource = src.DetectSource
	}
	if src.Hostname != "" && dst.Hostname == "" {
		dst.Hostname = src.Hostname
	}
	return dst
}

func FillIdentityGaps(dst, src *model.Inventory) *model.Inventory {
	if dst == nil {
		dst = &model.Inventory{}
	}
	if src == nil {
		return dst
	}
	fill := func(cur *string, v string) {
		if strings.TrimSpace(*cur) == "" && strings.TrimSpace(v) != "" {
			*cur = strings.TrimSpace(v)
		}
	}
	fill(&dst.Vendor, src.Vendor)
	fill(&dst.Product, src.Product)
	fill(&dst.ProductVersion, src.ProductVersion)
	fill(&dst.Serial, src.Serial)
	fill(&dst.SKU, src.SKU)
	fill(&dst.UUID, src.UUID)
	fill(&dst.Family, src.Family)
	fill(&dst.BoardVendor, src.BoardVendor)
	fill(&dst.BoardName, src.BoardName)
	fill(&dst.BoardSerial, src.BoardSerial)
	fill(&dst.AssetTag, src.AssetTag)
	fill(&dst.BIOSVendor, src.BIOSVendor)
	fill(&dst.BIOSVersion, src.BIOSVersion)
	fill(&dst.BIOSDate, src.BIOSDate)
	if dst.DetectSource == "" {
		dst.DetectSource = src.DetectSource
	}
	return dst
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
