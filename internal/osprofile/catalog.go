package osprofile

import "strings"

const (
	Netplan  = "netplan"
	Ifupdown = "ifupdown"
	NM       = "nm"
	Ifcfg    = "ifcfg"
)

type Version struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	DefaultUser string `json:"default_user"`
	RootFS      string `json:"root_fs"`
	NetBackend  string `json:"net_backend"`
}

type Distro struct {
	Family   string    `json:"family"`
	Label    string    `json:"label"`
	Versions []Version `json:"versions"`
}

func Catalog() []Distro {
	return []Distro{
		{Family: "ubuntu", Label: "Ubuntu", Versions: []Version{
			{ID: "20.04", Label: "20.04 LTS", DefaultUser: "root", RootFS: "ext4", NetBackend: Netplan},
			{ID: "22.04", Label: "22.04 LTS", DefaultUser: "root", RootFS: "ext4", NetBackend: Netplan},
			{ID: "24.04", Label: "24.04 LTS", DefaultUser: "root", RootFS: "ext4", NetBackend: Netplan},
			{ID: "26.04", Label: "26.04 LTS", DefaultUser: "root", RootFS: "ext4", NetBackend: Netplan},
		}},
		{Family: "debian", Label: "Debian", Versions: []Version{
			{ID: "11", Label: "11 (bullseye)", DefaultUser: "root", RootFS: "ext4", NetBackend: Ifupdown},
			{ID: "12", Label: "12 (bookworm)", DefaultUser: "root", RootFS: "ext4", NetBackend: Ifupdown},
			{ID: "13", Label: "13 (trixie)", DefaultUser: "root", RootFS: "ext4", NetBackend: Netplan},
		}},
		{Family: "rocky", Label: "Rocky Linux", Versions: []Version{
			{ID: "8", Label: "8", DefaultUser: "root", RootFS: "xfs", NetBackend: Ifcfg},
			{ID: "9", Label: "9", DefaultUser: "root", RootFS: "xfs", NetBackend: NM},
			{ID: "10", Label: "10", DefaultUser: "root", RootFS: "xfs", NetBackend: NM},
		}},
		{Family: "alma", Label: "AlmaLinux", Versions: []Version{
			{ID: "8", Label: "8", DefaultUser: "root", RootFS: "xfs", NetBackend: Ifcfg},
			{ID: "9", Label: "9", DefaultUser: "root", RootFS: "xfs", NetBackend: NM},
		}},
		{Family: "centos", Label: "CentOS", Versions: []Version{
			{ID: "7", Label: "7", DefaultUser: "root", RootFS: "ext4", NetBackend: Ifcfg},
			{ID: "9", Label: "Stream 9", DefaultUser: "root", RootFS: "xfs", NetBackend: NM},
		}},
		{Family: "custom", Label: "自定义", Versions: []Version{
			{ID: "generic", Label: "generic", DefaultUser: "root", RootFS: "ext4", NetBackend: Netplan},
		}},
	}
}

func CanonicalFamily(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "almalinux", "alma linux":
		return "alma"
	case "rocky linux", "rockylinux", "rhel", "redhat":
		return "rocky"
	case "centos stream":
		return "centos"
	case "":
		return "ubuntu"
	default:
		return s
	}
}

func Lookup(family, version string) Version {
	family = CanonicalFamily(family)
	version = strings.TrimSpace(version)
	for _, d := range Catalog() {
		if d.Family != family {
			continue
		}
		if version == "" {
			return d.Versions[len(d.Versions)-1]
		}
		for _, v := range d.Versions {
			if v.ID == version {
				return v
			}
		}
		return d.Versions[len(d.Versions)-1]
	}
	return Version{ID: version, Label: family, DefaultUser: "root", RootFS: "ext4", NetBackend: Netplan}
}

func Label(family, version string) string {
	family = CanonicalFamily(family)
	v := Lookup(family, version)
	for _, d := range Catalog() {
		if d.Family == family {
			if v.ID != "" && v.ID != "generic" {
				return d.Label + " " + v.Label
			}
			return d.Label
		}
	}
	if family == "" {
		return ""
	}
	if version != "" {
		return family + " " + version
	}
	return family
}
