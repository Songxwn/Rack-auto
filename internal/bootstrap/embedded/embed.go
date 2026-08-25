package embedded

import "embed"

// Firmware is stock iPXE from github.com/ipxe/ipxe releases (ipxeboot.tar.gz).
//
//go:embed undionly.kpxe ipxe.efi snponly.efi ipxe-arm64.efi snponly-arm64.efi NOTICE
var FS embed.FS

// Files maps TFTP filename -> embed name.
var Files = map[string]string{
	"undionly.kpxe":    "undionly.kpxe",
	"ipxe.efi":         "ipxe.efi",
	"snponly.efi":      "snponly.efi",
	"ipxe-arm64.efi":   "ipxe-arm64.efi",
	"snponly-arm64.efi": "snponly-arm64.efi",
}
