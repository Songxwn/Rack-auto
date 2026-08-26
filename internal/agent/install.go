package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Songxwn/Rack-auto/internal/cryptpw"
	"github.com/Songxwn/Rack-auto/internal/imageinspect"
	"github.com/Songxwn/Rack-auto/internal/model"
	"github.com/Songxwn/Rack-auto/internal/osprofile"
	"github.com/Songxwn/Rack-auto/internal/provision"
)

func (c *Client) RunInstall(ctx context.Context, job *model.AgentJob) error {
	spec, err := DecodeJSON[model.InstallSpec](job.Params)
	if err != nil {
		return fmt.Errorf("parse install params: %w", err)
	}
	if runtime.GOOS != "linux" {
		return fmt.Errorf("install agent requires Linux RAMOS")
	}
	if job.Image != nil && job.Image.IsWindows() {
		return fmt.Errorf("Windows Server installs via WinPE PXE, not Linux RAMOS")
	}
	if !isRoot() {
		return fmt.Errorf("install requires root")
	}
	log := func(format string, a ...any) {
		line := fmt.Sprintf(format, a...)
		fmt.Println(line)
		c.Log(job.ID, line)
	}
	progress := func(n int, msg string) {
		log(msg)
		c.Progress(job.ID, n, msg)
	}

	disk := spec.Disk
	if disk == "" {
		inv := CollectInventory()
		disk = pickLargestDisk(inv.Disks)
	}
	if disk == "" {
		return fmt.Errorf("no target disk")
	}

	kind := model.ImageCloudRoot
	var imageURL, checksum, checksumType, family, version string
	if job.Image != nil {
		if job.Image.Kind != "" {
			kind = job.Image.Kind
		}
		imageURL = job.Image.URL
		checksum = job.Image.Checksum
		checksumType = job.Image.ChecksumType
		family = job.Image.OSFamily
		version = job.Image.OSVersion
	}
	whole := kind == model.ImageRawDisk || kind == model.ImageCloudDisk
	if !whole {
		if len(spec.Partitions) == 0 {
			fw := spec.Firmware
			if fw == "" {
				fw = firmware()
			}
			spec.Partitions = provision.DefaultPartitions(fw, family, version)
		}
		if err := provision.Validate(spec.Partitions); err != nil {
			return err
		}
	}

	progress(5, "target disk "+disk)
	progress(8, "unmounting partitions")
	_ = umountDisk(disk)

	if imageURL == "" {
		return fmt.Errorf("image URL is empty")
	}

	if kind == model.ImageRawDisk || kind == model.ImageCloudDisk {
		progress(12, "download disk image")
		tmp := "/tmp/rackauto-image"
		if err := downloadFile(ctx, imageURL, tmp, func(got, total int64) {
			if total > 0 {
				c.Progress(job.ID, 12+int(got*30/total), fmt.Sprintf("download image %d/%d MB", got>>20, total>>20))
			}
		}); err != nil {
			return err
		}
		if err := verifyChecksum(tmp, checksum, checksumType); err != nil {
			return err
		}
		progress(45, "write "+disk)
		if err := writeDiskImage(log, tmp, disk, func(copied, total int64) {
			if total > 0 {
				c.Progress(job.ID, 45+int(copied*20/total), fmt.Sprintf("write disk %d/%d MB", copied>>20, total>>20))
			}
		}); err != nil {
			return fmt.Errorf("write disk: %w", err)
		}
		_ = run(log, "partprobe", disk)
		time.Sleep(2 * time.Second)
		hint := 0
		if job.Image != nil && job.Image.Inspect != nil {
			hint = job.Image.Inspect.RootNum
		}
		rootDev, err := findRootPartition(disk, hint)
		if err != nil {
			return err
		}
		log("root partition %s", rootDev)
		progress(68, "expand root to full disk")
		if err := growDisk(log, disk, rootDev, hint); err != nil {
			log("warn: grow partition: %v", err)
		}
		progress(70, "inject system config")
		if err := injectConfig(log, rootDev, disk, spec, job.Image, true); err != nil {
			return err
		}
		progress(88, "UEFI fallback boot files")
		if err := ensureUEFIBoot(log, disk, spec.Firmware); err != nil {
			if strings.EqualFold(spec.Firmware, model.FirmwareUEFI) {
				return fmt.Errorf("UEFI boot files: %w", err)
			}
			log("warn: UEFI boot setup: %v", err)
		}
		progress(95, "config done")
		if spec.Reboot {
			progress(98, "rebooting into installed OS")
			go func() { time.Sleep(3 * time.Second); _ = exec.Command("reboot").Run() }()
		}
		return nil
	}

	progress(10, "partition "+disk)
	for _, cmd := range provision.SGDiskScript(disk, spec.Partitions) {
		if err := runShell(log, cmd); err != nil {
			return fmt.Errorf("partition failed: %w", err)
		}
	}
	progress(20, "format partitions")
	for _, cmd := range provision.FormatCommands(disk, spec.Partitions) {
		if err := runShell(log, cmd); err != nil {
			return err
		}
	}

	rootIdx := rootIndex(spec.Partitions)
	rootDev := provision.PartitionPath(disk, rootIdx+1)
	tmp := "/tmp/rackauto-rootimg"
	progress(25, "download rootfs image")
	if err := downloadFile(ctx, imageURL, tmp, func(got, total int64) {
		if total > 0 {
			c.Progress(job.ID, 25+int(got*25/total), fmt.Sprintf("download image %d/%d MB", got>>20, total>>20))
		}
	}); err != nil {
		return err
	}
	if err := verifyChecksum(tmp, checksum, checksumType); err != nil {
		return err
	}
	progress(52, "write root "+rootDev)
	if err := writeDiskImage(log, tmp, rootDev, func(copied, total int64) {
		if total > 0 {
			c.Progress(job.ID, 52+int(copied*16/total), fmt.Sprintf("write root %d/%d MB", copied>>20, total>>20))
		}
	}); err != nil {
		return err
	}
	_ = run(log, "e2fsck", "-fp", rootDev)
	if probeDevFS(rootDev) == "xfs" {
		log("xfs root; grow after mount")
	} else {
		_ = run(log, "resize2fs", rootDev)
	}
	progress(72, "inject system config")
	if err := injectConfig(log, rootDev, disk, spec, job.Image, false); err != nil {
		return err
	}
	progress(90, "install bootloader")
	if err := installBootloader(log, disk, rootDev, spec); err != nil {
		log("warn: bootloader install failed: %v", err)
	}
	if err := ensureUEFIBoot(log, disk, spec.Firmware); err != nil {
		if strings.EqualFold(spec.Firmware, model.FirmwareUEFI) {
			return fmt.Errorf("UEFI boot files: %w", err)
		}
		log("warn: UEFI boot setup: %v", err)
	}
	if spec.Reboot {
		progress(98, "rebooting")
		go func() { time.Sleep(3 * time.Second); _ = exec.Command("reboot").Run() }()
	}
	return nil
}

func pickLargestDisk(disks []model.Disk) string {
	var best model.Disk
	for _, d := range disks {
		if d.SizeB > best.SizeB {
			best = d
		}
	}
	return best.Path
}

func rootIndex(parts []model.Partition) int {
	for i, p := range parts {
		if p.Mount == "/" {
			return i
		}
	}
	return len(parts) - 1
}

func verifyChecksum(path, sum, kind string) error {
	if sum == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	kind = strings.ToLower(kind)
	if kind == "" || kind == "sha256" {
		h := sha256.New()
		if _, err := io.Copy(h, f); err != nil {
			return err
		}
		got := hex.EncodeToString(h.Sum(nil))
		if !strings.EqualFold(got, strings.TrimSpace(sum)) {
			return fmt.Errorf("checksum mismatch")
		}
	}
	return nil
}

func downloadFile(ctx context.Context, url, dest string, cb func(got, total int64)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("download image failed: %s", resp.Status)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	var got int64
	buf := make([]byte, 1<<20)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				return werr
			}
			got += int64(n)
			if cb != nil {
				cb(got, resp.ContentLength)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func umountDisk(disk string) error {
	b, _ := os.ReadFile("/proc/mounts")
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, disk) {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				_ = exec.Command("umount", "-f", fields[1]).Run()
			}
		}
	}
	return nil
}

func findRootPartition(disk string, hint int) (string, error) {
	_ = exec.Command("partprobe", disk).Run()
	time.Sleep(time.Second)
	if hint > 0 {
		p := provision.PartitionPath(disk, hint)
		if _, err := os.Stat(p); err == nil {
			if fs := probeDevFS(p); fs == "ext4" || fs == "xfs" || fs == "btrfs" || fs == "" {
				return p, nil
			}
		}
	}
	parts := listDiskPartitions(disk)
	if len(parts) == 0 {
		return "", fmt.Errorf("no partition found after writing disk image")
	}
	var best string
	var bestSize int64
	for _, p := range parts {
		fs := probeDevFS(p)
		if fs == "vfat" || fs == "swap" {
			continue
		}
		sz := blockSize(p)
		score := sz
		if fs == "ext4" || fs == "xfs" || fs == "btrfs" {
			score += 1 << 40
		}
		if score > bestSize {
			bestSize = score
			best = p
		}
	}
	if best == "" {
		best = parts[len(parts)-1]
		var sz int64
		for _, p := range parts {
			if n := blockSize(p); n > sz {
				sz = n
				best = p
			}
		}
	}
	return best, nil
}

func listDiskPartitions(disk string) []string {
	base := filepath.Base(disk)
	ents, err := os.ReadDir("/sys/class/block")
	if err != nil {
		var out []string
		for i := 1; i <= 32; i++ {
			p := provision.PartitionPath(disk, i)
			if _, err := os.Stat(p); err == nil {
				out = append(out, p)
			}
		}
		return out
	}
	var out []string
	for _, e := range ents {
		name := e.Name()
		if name == base || !strings.HasPrefix(name, base) {
			continue
		}
		rest := strings.TrimPrefix(name, base)
		if rest == "" {
			continue
		}
		if rest[0] != 'p' && (rest[0] < '0' || rest[0] > '9') {
			continue
		}
		p := "/dev/" + name
		if _, err := os.Stat(p); err == nil {
			out = append(out, p)
		}
	}
	return out
}

func probeDevFS(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	return imageinspect.ProbeFS(f, 0)
}

func blockSize(dev string) int64 {
	b, err := os.ReadFile("/sys/class/block/" + filepath.Base(dev) + "/size")
	if err != nil {
		return 0
	}
	var n int64
	fmt.Sscanf(strings.TrimSpace(string(b)), "%d", &n)
	return n * 512
}

func injectConfig(log func(string, ...any), rootDev, disk string, spec model.InstallSpec, img *model.Image, keepLayout bool) error {
	mnt := "/mnt/target"
	_ = os.MkdirAll(mnt, 0o755)
	if err := run(log, "mount", rootDev, mnt); err != nil {
		_ = run(log, "mount", "-o", "nouuid", rootDev, mnt)
	}
	defer func() { _ = exec.Command("umount", "-R", mnt).Run() }()

	switch probeDevFS(rootDev) {
	case "xfs":
		_ = run(log, "xfs_growfs", "-d", mnt)
	case "btrfs":
		_ = run(log, "btrfs", "filesystem", "resize", "max", mnt)
	}

	if !keepLayout {
		for i, p := range spec.Partitions {
			if p.Mount == "" || p.Mount == "/" || p.FS == "swap" || p.FS == "biosboot" {
				continue
			}
			dev := provision.PartitionPath(disk, i+1)
			mp := filepath.Join(mnt, strings.TrimPrefix(p.Mount, "/"))
			_ = os.MkdirAll(mp, 0o755)
			_ = run(log, "mount", dev, mp)
		}
		_ = os.WriteFile(filepath.Join(mnt, "etc/fstab"), []byte(provision.Fstab(spec.Partitions, disk)), 0644)
	} else {
		log("keeping image fstab")
	}

	if spec.Hostname != "" {
		_ = os.WriteFile(filepath.Join(mnt, "etc/hostname"), []byte(spec.Hostname+"\n"), 0644)
	}

	hashed := ""
	if spec.Password != "" {
		var err error
		hashed, err = cryptpw.SHA512(spec.Password)
		if err != nil {
			return err
		}
	}
	userData, meta := provision.CloudInit(spec, hashed)
	seed := filepath.Join(mnt, "var/lib/cloud/seed/nocloud")
	_ = os.MkdirAll(seed, 0o755)
	_ = os.WriteFile(filepath.Join(seed, "user-data"), []byte(userData), 0644)
	_ = os.WriteFile(filepath.Join(seed, "meta-data"), []byte(meta), 0644)
	_ = os.MkdirAll(filepath.Join(mnt, "etc/cloud/cloud.cfg.d"), 0o755)
	_ = os.WriteFile(filepath.Join(mnt, "etc/cloud/cloud.cfg.d/99-rackauto.cfg"), []byte("datasource_list: [NoCloud]\n"), 0644)

	backend := osprofile.Netplan
	if img != nil {
		backend = osprofile.Lookup(img.OSFamily, img.OSVersion).NetBackend
	}
	if err := provision.ApplyNetwork(mnt, spec.Network, backend); err != nil {
		return err
	}
	log("network backend %s", backend)

	user := spec.Username
	if user == "" {
		user = "root"
	}
	home := "/root"
	if user != "root" {
		home = "/home/" + user
		_ = os.MkdirAll(filepath.Join(mnt, home), 0o755)
	}
	if len(spec.SSHKeys) > 0 {
		sshDir := filepath.Join(mnt, home, ".ssh")
		_ = os.MkdirAll(sshDir, 0o700)
		var keys strings.Builder
		for _, k := range spec.SSHKeys {
			k = strings.TrimSpace(k)
			if k != "" {
				keys.WriteString(k + "\n")
			}
		}
		_ = os.WriteFile(filepath.Join(sshDir, "authorized_keys"), []byte(keys.String()), 0o600)
	}
	if hashed != "" {
		shadow := filepath.Join(mnt, "etc/shadow")
		if b, err := os.ReadFile(shadow); err == nil {
			lines := strings.Split(string(b), "\n")
			found := false
			for i, line := range lines {
				if strings.HasPrefix(line, user+":") {
					rest := strings.SplitN(line, ":", 3)
					if len(rest) >= 3 {
						lines[i] = user + ":" + hashed + ":" + rest[2]
					} else {
						lines[i] = user + ":" + hashed + ":1:0:99999:7:::"
					}
					found = true
				}
			}
			if !found {
				lines = append(lines, user+":"+hashed+":1:0:99999:7:::")
			}
			_ = os.WriteFile(shadow, []byte(strings.Join(lines, "\n")), 0640)
		}
	}
	_ = os.MkdirAll(filepath.Join(mnt, "etc/ssh/sshd_config.d"), 0o755)
	_ = os.WriteFile(filepath.Join(mnt, "etc/ssh/sshd_config.d/99-rackauto.conf"), []byte("PasswordAuthentication yes\nPermitRootLogin yes\n"), 0644)
	log("wrote hostname / SSH / password / NIC / cloud-init")
	return nil
}

func installBootloader(log func(string, ...any), disk, rootDev string, spec model.InstallSpec) error {
	fw := spec.Firmware
	if fw == "" {
		fw = firmware()
	}
	mnt := "/mnt/target"
	_ = os.MkdirAll(mnt, 0o755)
	if err := run(log, "mount", rootDev, mnt); err != nil {
		if err := run(log, "mount", "-o", "nouuid", rootDev, mnt); err != nil {
			return err
		}
	}
	defer func() { _ = exec.Command("umount", "-R", mnt).Run() }()
	if fw == model.FirmwareUEFI {
		esp, _, err := findESP(disk)
		if err != nil {
			return err
		}
		efi := filepath.Join(mnt, "boot", "efi")
		_ = os.MkdirAll(efi, 0o755)
		if err := run(log, "mount", esp, efi); err != nil {
			return fmt.Errorf("mount ESP: %w", err)
		}
	}
	for _, c := range []string{
		"mount --bind /dev " + mnt + "/dev",
		"mount --bind /proc " + mnt + "/proc",
		"mount --bind /sys " + mnt + "/sys",
	} {
		_ = runShell(log, c)
	}
	if fw == model.FirmwareUEFI {
		return runShell(log, "chroot "+mnt+" grub-install --target=x86_64-efi --efi-directory=/boot/efi --bootloader-id=BOOT --removable --recheck && chroot "+mnt+" grub-mkconfig -o /boot/grub/grub.cfg")
	}
	return runShell(log, "chroot "+mnt+" grub-install --target=i386-pc --recheck "+disk+" && chroot "+mnt+" grub-mkconfig -o /boot/grub/grub.cfg")
}

func run(log func(string, ...any), name string, args ...string) error {
	log("+ %s %s", name, strings.Join(args, " "))
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if len(out) > 0 {
		log("%s", strings.TrimSpace(string(out)))
	}
	return err
}

func runShell(log func(string, ...any), line string) error {
	log("+ %s", line)
	cmd := exec.Command("sh", "-c", line)
	out, err := cmd.CombinedOutput()
	if len(out) > 0 {
		log("%s", strings.TrimSpace(string(out)))
	}
	return err
}
