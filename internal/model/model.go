package model

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	MachineDiscovered  = "discovered"
	MachineReady       = "ready"
	MachineInstalling  = "installing"
	MachineStressing   = "stressing"
	MachineProvisioned = "provisioned"
	MachineOffline     = "offline"
	MachineError       = "error"

	FirmwareBIOS = "bios"
	FirmwareUEFI = "uefi"

	BootPXE  = "pxe"
	BootDisk = "disk"
	BootRAM  = "ramos"

	BMCIPMI    = "ipmi"
	BMCRedfish = "redfish"

	JobInstall = "install"
	JobStress  = "stress"

	JobPending   = "pending"
	JobRunning   = "running"
	JobSucceeded = "succeeded"
	JobFailed    = "failed"
	JobCancelled = "cancelled"

	ImageCloudDisk   = "cloud-disk"
	ImageCloudRoot   = "cloud-root"
	ImageRawDisk     = "raw-disk"
	ImageWindowsISO  = "windows-iso"
	ImageWindowsWIM  = "windows-wim"

	NICEthernet = "ethernet"
	NICBond     = "bond"
	NICVLAN     = "vlan"

	TemplateAccount = "account"
	TemplateKey     = "key"
)

type Machine struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	MAC          string     `json:"mac"`
	IP           string     `json:"ip"`
	Status       string     `json:"status"`
	Firmware     string     `json:"firmware"`
	BootMode     string     `json:"boot_mode"`
	BMCType      string     `json:"bmc_type"`
	BMCAddress   string     `json:"bmc_address"`
	BMCPort      int        `json:"bmc_port"`
	BMCUsername  string     `json:"bmc_username"`
	BMCPassword  string     `json:"bmc_password,omitempty"`
	BMCInsecure  bool       `json:"bmc_insecure"`
	Tags         []string   `json:"tags"`
	Inventory    *Inventory `json:"inventory,omitempty"`
	Notes        string     `json:"notes"`
	AgentVersion string     `json:"agent_version"`
	LastSeen     *time.Time `json:"last_seen,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

func (m Machine) Public() Machine {
	m.BMCPassword = ""
	return m
}

type Inventory struct {
	Hostname       string `json:"hostname"`
	Firmware       string `json:"firmware"`
	Arch           string `json:"arch"`
	CPUs           int    `json:"cpus"`
	CPUModel       string `json:"cpu_model"`
	MemoryMB       int    `json:"memory_mb"`
	Disks          []Disk `json:"disks"`
	NICs           []NIC  `json:"nics"`
	BMC            string `json:"bmc,omitempty"`
	Kernel         string `json:"kernel"`
	UptimeSec      int64  `json:"uptime_sec"`
	Vendor         string `json:"vendor,omitempty"`
	Product        string `json:"product,omitempty"`
	ProductVersion string `json:"product_version,omitempty"`
	Serial         string `json:"serial,omitempty"`
	SKU            string `json:"sku,omitempty"`
	UUID           string `json:"uuid,omitempty"`
	Family         string `json:"family,omitempty"`
	BoardVendor    string `json:"board_vendor,omitempty"`
	BoardName      string `json:"board_name,omitempty"`
	BoardSerial    string `json:"board_serial,omitempty"`
	AssetTag       string `json:"asset_tag,omitempty"`
	BIOSVendor     string `json:"bios_vendor,omitempty"`
	BIOSVersion    string `json:"bios_version,omitempty"`
	BIOSDate       string `json:"bios_date,omitempty"`
	DetectSource   string `json:"detect_source,omitempty"`
}

func (inv *Inventory) HasIdentity() bool {
	return inv != nil && (inv.Vendor != "" || inv.Product != "" || inv.Serial != "")
}

func (inv *Inventory) ProductLine() string {
	if inv == nil {
		return ""
	}
	v, p := strings.TrimSpace(inv.Vendor), strings.TrimSpace(inv.Product)
	switch {
	case v != "" && p != "":
		if strings.HasPrefix(strings.ToLower(p), strings.ToLower(v)) {
			return p
		}
		return v + " " + p
	case p != "":
		return p
	default:
		return v
	}
}

func (inv *Inventory) IdentityName() string {
	if inv == nil {
		return ""
	}
	if line := inv.ProductLine(); line != "" {
		return line
	}
	if inv.Serial != "" {
		return "SN " + inv.Serial
	}
	return ""
}

type Disk struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	SizeB  int64  `json:"size_b"`
	Model  string `json:"model"`
	Rot    bool   `json:"rotational"`
	Serial string `json:"serial"`
}

type NIC struct {
	Name  string   `json:"name"`
	MAC   string   `json:"mac"`
	IPs   []string `json:"ips"`
	MTU   int      `json:"mtu"`
	Up    bool     `json:"up"`
	Speed string   `json:"speed"`
}

type Image struct {
	ID           string        `json:"id"`
	Name         string        `json:"name"`
	OSFamily     string        `json:"os_family"`
	OSVersion    string        `json:"os_version,omitempty"`
	Kind         string        `json:"kind"`
	URL          string        `json:"url"`
	Filename     string        `json:"filename"`
	Checksum     string        `json:"checksum"`
	ChecksumType string        `json:"checksum_type"`
	SizeB        int64         `json:"size_b"`
	Notes        string        `json:"notes"`
	Inspect      *ImageInspect `json:"inspect,omitempty"`
	CreatedAt    time.Time     `json:"created_at"`
}

type ImageInspect struct {
	Status       string            `json:"status"`
	Format       string            `json:"format,omitempty"`
	Table        string            `json:"table,omitempty"`
	VirtualSizeB int64             `json:"virtual_size_b"`
	BootUEFI     bool              `json:"boot_uefi"`
	BootBIOS     bool              `json:"boot_bios"`
	EFILoader    string            `json:"efi_loader,omitempty"`
	RootFS       string            `json:"root_fs,omitempty"`
	RootNum      int               `json:"root_num,omitempty"`
	ESPNum       int               `json:"esp_num,omitempty"`
	Message      string            `json:"message,omitempty"`
	Warnings     []string           `json:"warnings,omitempty"`
	Partitions   []InspectPartition `json:"partitions,omitempty"`
	Windows      bool               `json:"windows,omitempty"`
	WIMImages    []WIMImage         `json:"wim_images,omitempty"`
	BootWIM      string             `json:"boot_wim,omitempty"`
	InstallWIM   string             `json:"install_wim,omitempty"`
	InstallFrom  string             `json:"install_from,omitempty"`
	InstallOff   int64              `json:"install_offset,omitempty"`
	InstallSize  int64              `json:"install_size,omitempty"`
	InspectedAt  time.Time          `json:"inspected_at"`
}

type WIMImage struct {
	Index       int    `json:"index"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Flags       string `json:"flags,omitempty"`
	Edition     string `json:"edition,omitempty"`
	Arch        string `json:"arch,omitempty"`
	SizeB       int64  `json:"size_b,omitempty"`
}

type InspectPartition struct {
	Number int    `json:"number"`
	Type   string `json:"type"`
	FS     string `json:"fs,omitempty"`
	SizeB  int64  `json:"size_b"`
	StartB int64  `json:"start_b"`
}

func IsWindowsKind(kind string) bool {
	return kind == ImageWindowsISO || kind == ImageWindowsWIM
}

func (img Image) IsWindows() bool {
	if IsWindowsKind(img.Kind) {
		return true
	}
	if img.Inspect != nil && img.Inspect.Windows {
		return true
	}
	f := strings.ToLower(strings.TrimSpace(img.OSFamily))
	return f == "windows" || strings.HasPrefix(f, "windows")
}

func (in *ImageInspect) Compatible(kind, firmware string) error {
	if in == nil || in.Status == "" || in.Status == "skipped" {
		return nil
	}
	if in.Status == "error" {
		return fmt.Errorf("image inspect failed: %s", in.Message)
	}
	if IsWindowsKind(kind) || in.Windows {
		if in.Windows && len(in.WIMImages) == 0 && in.Status == "ok" {
			return fmt.Errorf("Windows image has no WIM editions; re-inspect or upload install.wim")
		}
		return nil
	}
	whole := kind == ImageCloudDisk || kind == ImageRawDisk
	if whole {
		switch firmware {
		case FirmwareBIOS:
			if !in.BootBIOS {
				return fmt.Errorf("image has no BIOS bootloader; choose UEFI firmware or a BIOS-capable image")
			}
		default:
			if !in.BootUEFI {
				return fmt.Errorf("image has no UEFI ESP/EFI loader; choose BIOS firmware or a UEFI cloud image")
			}
		}
		if in.RootFS == "" {
			return fmt.Errorf("image has no Linux root filesystem")
		}
		return nil
	}
	if in.Table == "gpt" || in.Table == "mbr" {
		if in.BootUEFI || in.BootBIOS {
			return fmt.Errorf("image has a partition table and bootloader; use kind cloud-disk or raw-disk")
		}
	}
	if in.RootFS == "" {
		return fmt.Errorf("image has no Linux root filesystem; use a cloud-root ext4/xfs image")
	}
	return nil
}

type Job struct {
	ID         string     `json:"id"`
	Type       string     `json:"type"`
	MachineID  string     `json:"machine_id"`
	ImageID    string     `json:"image_id,omitempty"`
	Status     string     `json:"status"`
	Params     any        `json:"params"`
	Progress   int        `json:"progress"`
	Message    string     `json:"message"`
	Logs       string     `json:"logs,omitempty"`
	Result     any        `json:"result,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

type InstallSpec struct {
	ImageID    string      `json:"image_id"`
	Hostname   string      `json:"hostname"`
	Username   string      `json:"username"`
	Password   string      `json:"password"`
	SSHKeys    []string    `json:"ssh_keys"`
	Timezone   string      `json:"timezone"`
	Firmware   string      `json:"firmware"`
	Disk       string      `json:"disk"`
	Partitions []Partition `json:"partitions"`
	Network    NetConfig   `json:"network"`
	Reboot     bool        `json:"reboot"`
	WIMIndex   int         `json:"wim_index,omitempty"`
	ProductKey string      `json:"product_key,omitempty"`
	EnableRDP  bool        `json:"enable_rdp"`
}

type Partition struct {
	Name   string `json:"name"`
	SizeMB int    `json:"size_mb"`
	FS     string `json:"fs"`
	Mount  string `json:"mount"`
	Flags  string `json:"flags"`
}

type NetConfig struct {
	Hostname string      `json:"hostname"`
	NICs     []NICConfig `json:"nics"`
}

type NICConfig struct {
	Kind        string   `json:"kind,omitempty"`
	MAC         string   `json:"mac,omitempty"`
	Name        string   `json:"name"`
	Method      string   `json:"method"`
	Address     string   `json:"address,omitempty"`
	Gateway     string   `json:"gateway,omitempty"`
	DNS         []string `json:"dns,omitempty"`
	MTU         int      `json:"mtu,omitempty"`
	BondMode    string   `json:"bond_mode,omitempty"`
	BondMembers []string `json:"bond_members,omitempty"`
	VLANID      int      `json:"vlan_id,omitempty"`
	Parent      string   `json:"parent,omitempty"`
}

func (n NICConfig) Type() string {
	if n.Kind != "" {
		return n.Kind
	}
	if n.VLANID > 0 || n.Parent != "" {
		return NICVLAN
	}
	if len(n.BondMembers) > 0 || n.BondMode != "" {
		return NICBond
	}
	return NICEthernet
}

type StressSpec struct {
	Targets       []string `json:"targets"`
	DurationSec   int      `json:"duration_sec"`
	CPUWorkers    int      `json:"cpu_workers"`
	MemoryPercent int      `json:"memory_percent"`
	DiskPath      string   `json:"disk_path"`
	DiskSizeMB    int      `json:"disk_size_mb"`
}

type StressResult struct {
	CPU     *CPUResult     `json:"cpu,omitempty"`
	Memory  *MemoryResult  `json:"memory,omitempty"`
	Disk    *DiskResult    `json:"disk,omitempty"`
	Network *NetworkResult `json:"network,omitempty"`
}

type CPUResult struct {
	Workers  int     `json:"workers"`
	Hashes   uint64  `json:"hashes"`
	Seconds  float64 `json:"seconds"`
	HashRate float64 `json:"hash_rate"`
}

type MemoryResult struct {
	Bytes      int64   `json:"bytes"`
	Seconds    float64 `json:"seconds"`
	Errors     int     `json:"errors"`
	Throughput float64 `json:"mb_s"`
}

type DiskResult struct {
	Path     string  `json:"path"`
	SizeMB   int     `json:"size_mb"`
	WriteMBs float64 `json:"write_mb_s"`
	ReadMBs  float64 `json:"read_mb_s"`
	VerifyOK bool    `json:"verify_ok"`
	Seconds  float64 `json:"seconds"`
}

type NetworkResult struct {
	DownloadMBs float64 `json:"download_mb_s"`
	UploadMBs   float64 `json:"upload_mb_s"`
	Seconds     float64 `json:"seconds"`
}

type Event struct {
	ID        string    `json:"id"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
	MachineID string    `json:"machine_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type CredentialTemplate struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Name      string    `json:"name"`
	Username  string    `json:"username,omitempty"`
	Password  string    `json:"password,omitempty"`
	SSHKeys   []string  `json:"ssh_keys,omitempty"`
	Notes     string    `json:"notes,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Overview struct {
	Machines      int            `json:"machines"`
	Online        int            `json:"online"`
	Images        int            `json:"images"`
	Jobs          int            `json:"jobs"`
	Running       int            `json:"running"`
	ByStatus      map[string]int `json:"by_status"`
	DHCPRunning   bool           `json:"dhcp_running"`
	DHCPInterface string         `json:"dhcp_interface"`
}

type PowerRequest struct {
	Action string `json:"action"`
}

type BootRequest struct {
	Device     string `json:"device"`
	Firmware   string `json:"firmware"`
	Persistent bool   `json:"persistent"`
}

type AgentRegister struct {
	MAC          string     `json:"mac"`
	IP           string     `json:"ip"`
	Hostname     string     `json:"hostname"`
	AgentVersion string     `json:"agent_version"`
	Inventory    *Inventory `json:"inventory"`
}

type AgentJob struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Params any    `json:"params"`
	Image  *Image `json:"image,omitempty"`
}

func ParseInstallSpec(v any) (InstallSpec, error) {
	var spec InstallSpec
	b, err := json.Marshal(v)
	if err != nil {
		return spec, err
	}
	err = json.Unmarshal(b, &spec)
	return spec, err
}
