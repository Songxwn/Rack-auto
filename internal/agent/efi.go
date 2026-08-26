package agent

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Songxwn/Rack-auto/internal/diskgrow"
	"github.com/Songxwn/Rack-auto/internal/model"
)

// ensureUEFIBoot copies the OS bootloader to \EFI\BOOT\BOOTX64.EFI (or
// BOOTAA64.EFI). Cloned disks lose the VM's NVRAM Boot#### entry, so firmware
// only looks at the removable-media path and otherwise reports an empty ESP.
func ensureUEFIBoot(log func(string, ...any), disk, firmware string) error {
	esp, part, err := findESP(disk)
	if err != nil {
		if strings.EqualFold(firmware, model.FirmwareUEFI) {
			return err
		}
		log("no ESP: %v", err)
		return nil
	}
	log("ESP %s (p%d)", esp, part)
	_ = run(log, "sgdisk", "-t", fmt.Sprintf("%d:ef00", part), disk)
	_ = run(log, "partprobe", disk)
	mnt := "/mnt/rackauto-esp"
	if err := os.MkdirAll(mnt, 0o755); err != nil {
		return err
	}
	if err := run(log, "mount", "-t", "vfat", "-o", "rw,umask=0077", esp, mnt); err != nil {
		if err := run(log, "mount", esp, mnt); err != nil {
			return fmt.Errorf("mount ESP: %w", err)
		}
	}
	defer func() { _ = exec.Command("umount", mnt).Run() }()
	wrote, err := installEFIFallback(mnt)
	if err != nil {
		return err
	}
	for _, w := range wrote {
		log("UEFI fallback %s", w)
	}
	listEFI(log, mnt)
	registerEFIBoot(log, disk, part)
	return nil
}

func findESP(disk string) (string, int, error) {
	var best string
	var bestNum int
	var bestSize int64
	for _, p := range listDiskPartitions(disk) {
		if probeDevFS(p) != "vfat" {
			continue
		}
		n := diskgrow.PartNumFromPath(p)
		sz := blockSize(p)
		if sz <= 0 || sz > 4<<30 {
			continue
		}
		if best == "" || sz < bestSize {
			best, bestNum, bestSize = p, n, sz
		}
	}
	if best == "" {
		return "", 0, fmt.Errorf("no FAT EFI system partition on %s", disk)
	}
	if bestNum <= 0 {
		bestNum = 1
	}
	return best, bestNum, nil
}

func installEFIFallback(espMount string) ([]string, error) {
	bootName, shimName, grubName, mokName := efiFallbackNames()
	bootDir := filepath.Join(espMount, "EFI", "BOOT")
	if err := os.MkdirAll(bootDir, 0o755); err != nil {
		return nil, err
	}
	found := scanEFIFiles(espMount, bootName, shimName, grubName, mokName)
	var wrote []string
	dstBoot := filepath.Join(bootDir, bootName)
	if found.boot == "" {
		src := found.shim
		if src == "" {
			src = found.grub
		}
		if src == "" {
			return nil, fmt.Errorf("ESP has no %s / %s / %s", bootName, shimName, grubName)
		}
		if err := copyFile(src, dstBoot); err != nil {
			return nil, err
		}
		wrote = append(wrote, filepath.Base(src)+" -> EFI/BOOT/"+bootName)
	} else {
		wrote = append(wrote, "EFI/BOOT/"+bootName+" already present")
	}
	if found.grub != "" {
		dstGrub := filepath.Join(bootDir, grubName)
		if !sameFile(found.grub, dstGrub) {
			if err := copyFile(found.grub, dstGrub); err != nil {
				return nil, err
			}
			wrote = append(wrote, grubName+" -> EFI/BOOT/"+grubName)
		}
	}
	if found.mok != "" {
		dstMok := filepath.Join(bootDir, mokName)
		if _, err := os.Stat(dstMok); err != nil {
			if err := copyFile(found.mok, dstMok); err != nil {
				return nil, err
			}
			wrote = append(wrote, mokName+" -> EFI/BOOT/"+mokName)
		}
	}
	return wrote, nil
}

type efiFiles struct {
	boot, shim, grub, mok string
}

func efiFallbackNames() (boot, shim, grub, mok string) {
	if runtime.GOARCH == "arm64" {
		return "BOOTAA64.EFI", "shimaa64.efi", "grubaa64.efi", "mmaa64.efi"
	}
	return "BOOTX64.EFI", "shimx64.efi", "grubx64.efi", "mmx64.efi"
}

func scanEFIFiles(root, bootName, shimName, grubName, mokName string) efiFiles {
	var out efiFiles
	bootKey := strings.ToLower(bootName)
	shimKey := strings.ToLower(shimName)
	grubKey := strings.ToLower(grubName)
	mokKey := strings.ToLower(mokName)
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		base := strings.ToLower(info.Name())
		rel, _ := filepath.Rel(root, path)
		inBoot := strings.Contains(strings.ToUpper(filepath.ToSlash(rel)), "EFI/BOOT/")
		switch base {
		case bootKey, "bootx64.efi", "bootaa64.efi":
			if inBoot {
				out.boot = path
			}
		case shimKey, "shimx64.efi", "shimaa64.efi", "shim.efi":
			if !inBoot || out.shim == "" {
				out.shim = path
			}
		case grubKey, "grubx64.efi", "grubaa64.efi", "grub.efi":
			if !inBoot || out.grub == "" {
				out.grub = path
			}
		case mokKey, "mmx64.efi", "mmaa64.efi":
			if !inBoot || out.mok == "" {
				out.mok = path
			}
		}
		return nil
	})
	return out
}

func listEFI(log func(string, ...any), root string) {
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if strings.Contains(strings.ToUpper(filepath.ToSlash(rel)), "EFI/") {
			log("ESP file %s", filepath.ToSlash(rel))
		}
		return nil
	})
}

func registerEFIBoot(log func(string, ...any), disk string, part int) {
	if _, err := os.Stat("/sys/firmware/efi"); err != nil {
		log("no efivars; skip efibootmgr (firmware will use EFI/BOOT)")
		return
	}
	if _, err := exec.LookPath("efibootmgr"); err != nil {
		log("efibootmgr not installed")
		return
	}
	loader := `\EFI\BOOT\BOOTX64.EFI`
	if runtime.GOARCH == "arm64" {
		loader = `\EFI\BOOT\BOOTAA64.EFI`
	}
	_ = run(log, "efibootmgr", "-c", "-d", disk, "-p", fmt.Sprintf("%d", part), "-l", loader, "-L", "Linux")
}

func copyFile(src, dst string) error {
	if src == dst {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

func copyAll(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	defer out.Close()
	if st, err := out.Stat(); err == nil && st.Mode().IsRegular() {
		if err := out.Truncate(0); err != nil {
			return err
		}
	}
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

func sameFile(a, b string) bool {
	sa, err := os.Stat(a)
	if err != nil {
		return false
	}
	sb, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(sa, sb)
}
