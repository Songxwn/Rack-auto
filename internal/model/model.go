package model

import "time"

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

	ImageCloudDisk = "cloud-disk"
	ImageCloudRoot = "cloud-root"
	ImageRawDisk   = "raw-disk"
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
	Hostname  string `json:"hostname"`
	Firmware  string `json:"firmware"`
	Arch      string `json:"arch"`
	CPUs      int    `json:"cpus"`
	CPUModel  string `json:"cpu_model"`
	MemoryMB  int    `json:"memory_mb"`
	Disks     []Disk `json:"disks"`
	NICs      []NIC  `json:"nics"`
	BMC       string `json:"bmc,omitempty"`
	Kernel    string `json:"kernel"`
	UptimeSec int64  `json:"uptime_sec"`
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
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	OSFamily     string    `json:"os_family"`
	Kind         string    `json:"kind"`
	URL          string    `json:"url"`
	Filename     string    `json:"filename"`
	Checksum     string    `json:"checksum"`
	ChecksumType string    `json:"checksum_type"`
	SizeB        int64     `json:"size_b"`
	Notes        string    `json:"notes"`
	CreatedAt    time.Time `json:"created_at"`
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
	MAC     string   `json:"mac"`
	Name    string   `json:"name"`
	Method  string   `json:"method"`
	Address string   `json:"address"`
	Gateway string   `json:"gateway"`
	DNS     []string `json:"dns"`
	MTU     int      `json:"mtu"`
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
