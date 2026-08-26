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
	"github.com/Songxwn/Rack-auto/internal/model"
	"github.com/Songxwn/Rack-auto/internal/provision"
)

func (c *Client) RunInstall(ctx context.Context, job *model.AgentJob) error {
	spec, err := DecodeJSON[model.InstallSpec](job.Params)
	if err != nil {
		return fmt.Errorf("解析装机参数: %w", err)
	}
	if runtime.GOOS != "linux" {
		return fmt.Errorf("装机 Agent 仅支持 Linux RAMOS")
	}
	if !isRoot() {
		return fmt.Errorf("装机需要 root")
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
		return fmt.Errorf("未找到目标磁盘")
	}
	if len(spec.Partitions) == 0 {
		fw := spec.Firmware
		if fw == "" {
			fw = firmware()
		}
		spec.Partitions = provision.DefaultPartitions(fw)
	}
	if err := provision.Validate(spec.Partitions); err != nil {
		return err
	}

	progress(5, "目标磁盘 "+disk)
	progress(8, "卸载可能占用的分区")
	_ = umountDisk(disk)

	kind := model.ImageCloudRoot
	var imageURL, checksum, checksumType string
	if job.Image != nil {
		kind = job.Image.Kind
		imageURL = job.Image.URL
		checksum = job.Image.Checksum
		checksumType = job.Image.ChecksumType
	}
	if imageURL == "" {
		return fmt.Errorf("镜像 URL 为空")
	}

	if kind == model.ImageRawDisk || kind == model.ImageCloudDisk {
		progress(12, "下载并写入整盘镜像")
		tmp := "/tmp/rackauto-image"
		if err := downloadFile(ctx, imageURL, tmp, func(got, total int64) {
			if total > 0 {
				c.Progress(job.ID, 12+int(got*30/total), fmt.Sprintf("下载镜像 %d/%d MB", got>>20, total>>20))
			}
		}); err != nil {
			return err
		}
		if err := verifyChecksum(tmp, checksum, checksumType); err != nil {
			return err
		}
		progress(45, "写入 "+disk)
		if err := writeDiskImage(log, tmp, disk, func(copied, total int64) {
			if total > 0 {
				c.Progress(job.ID, 45+int(copied*20/total), fmt.Sprintf("写入磁盘 %d/%d MB", copied>>20, total>>20))
			}
		}); err != nil {
			return fmt.Errorf("写入磁盘失败: %w", err)
		}
		_ = run(log, "partprobe", disk)
		time.Sleep(2 * time.Second)
		rootDev, err := findRootPartition(disk)
		if err != nil {
			return err
		}
		progress(70, "注入系统配置")
		if err := injectConfig(log, rootDev, disk, spec); err != nil {
			return err
		}
		progress(95, "配置完成")
		if spec.Reboot {
			progress(98, "即将重启进入已安装系统")
			go func() { time.Sleep(3 * time.Second); _ = exec.Command("reboot").Run() }()
		}
		return nil
	}

	progress(10, "分区 " + disk)
	for _, cmd := range provision.SGDiskScript(disk, spec.Partitions) {
		if err := runShell(log, cmd); err != nil {
			return fmt.Errorf("分区失败: %w", err)
		}
	}
	progress(20, "格式化分区")
	for _, cmd := range provision.FormatCommands(disk, spec.Partitions) {
		if err := runShell(log, cmd); err != nil {
			return err
		}
	}

	rootIdx := rootIndex(spec.Partitions)
	rootDev := provision.PartitionPath(disk, rootIdx+1)
	tmp := "/tmp/rackauto-rootimg"
	progress(25, "下载根文件系统镜像")
	if err := downloadFile(ctx, imageURL, tmp, func(got, total int64) {
		if total > 0 {
			c.Progress(job.ID, 25+int(got*25/total), fmt.Sprintf("下载镜像 %d/%d MB", got>>20, total>>20))
		}
	}); err != nil {
		return err
	}
	if err := verifyChecksum(tmp, checksum, checksumType); err != nil {
		return err
	}
	progress(52, "写入根分区 "+rootDev)
	if err := writeDiskImage(log, tmp, rootDev, func(copied, total int64) {
		if total > 0 {
			c.Progress(job.ID, 52+int(copied*16/total), fmt.Sprintf("写入根分区 %d/%d MB", copied>>20, total>>20))
		}
	}); err != nil {
		return err
	}
	_ = run(log, "e2fsck", "-fp", rootDev)
	_ = run(log, "resize2fs", rootDev)
	progress(72, "注入系统配置")
	if err := injectConfig(log, rootDev, disk, spec); err != nil {
		return err
	}
	progress(90, "安装引导程序")
	if err := installBootloader(log, disk, spec); err != nil {
		log("警告: 引导安装失败: %v（云镜像可能已自带引导）", err)
	}
	if spec.Reboot {
		progress(98, "即将重启")
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
			return fmt.Errorf("校验和不匹配")
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
		return fmt.Errorf("下载镜像失败: %s", resp.Status)
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

func findRootPartition(disk string) (string, error) {
	_ = exec.Command("partprobe", disk).Run()
	time.Sleep(time.Second)
	candidates := []string{}
	for i := 1; i <= 8; i++ {
		p := provision.PartitionPath(disk, i)
		if _, err := os.Stat(p); err == nil {
			candidates = append(candidates, p)
		}
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("整盘镜像写入后未发现分区")
	}
	best := candidates[len(candidates)-1]
	var bestSize int64
	for _, p := range candidates {
		if sz := blockSize(p); sz > bestSize {
			bestSize = sz
			best = p
		}
	}
	return best, nil
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

func injectConfig(log func(string, ...any), rootDev, disk string, spec model.InstallSpec) error {
	mnt := "/mnt/target"
	_ = os.MkdirAll(mnt, 0o755)
	if err := run(log, "mount", rootDev, mnt); err != nil {
		_ = run(log, "mount", "-o", "nouuid", rootDev, mnt)
	}
	defer func() { _ = exec.Command("umount", "-R", mnt).Run() }()

	for i, p := range spec.Partitions {
		if p.Mount == "" || p.Mount == "/" || p.FS == "swap" || p.FS == "biosboot" {
			continue
		}
		dev := provision.PartitionPath(disk, i+1)
		mp := filepath.Join(mnt, strings.TrimPrefix(p.Mount, "/"))
		_ = os.MkdirAll(mp, 0o755)
		_ = run(log, "mount", dev, mp)
	}

	if spec.Hostname != "" {
		_ = os.WriteFile(filepath.Join(mnt, "etc/hostname"), []byte(spec.Hostname+"\n"), 0644)
	}
	_ = os.WriteFile(filepath.Join(mnt, "etc/fstab"), []byte(provision.Fstab(spec.Partitions, disk)), 0644)

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

	netplan := provision.Netplan(spec.Network)
	_ = os.MkdirAll(filepath.Join(mnt, "etc/netplan"), 0o755)
	_ = os.WriteFile(filepath.Join(mnt, "etc/netplan/99-rackauto.yaml"), []byte(netplan), 0644)
	_ = os.MkdirAll(filepath.Join(mnt, "etc/network"), 0o755)
	_ = os.WriteFile(filepath.Join(mnt, "etc/network/interfaces"), []byte(provision.Ifupdown(spec.Network)), 0644)
	_ = os.WriteFile(filepath.Join(mnt, "etc/cloud/cloud.cfg.d/99-disable-network-config.cfg"), []byte("network: {config: disabled}\n"), 0644)

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
	log("已写入 hostname / SSH / 密码 / 网卡 / cloud-init")
	return nil
}

func installBootloader(log func(string, ...any), disk string, spec model.InstallSpec) error {
	fw := spec.Firmware
	if fw == "" {
		fw = firmware()
	}
	cmds := []string{
		"mount --bind /dev /mnt/target/dev",
		"mount --bind /proc /mnt/target/proc",
		"mount --bind /sys /mnt/target/sys",
	}
	for _, c := range cmds {
		_ = runShell(log, c)
	}
	if fw == model.FirmwareUEFI {
		return runShell(log, "chroot /mnt/target grub-install --target=x86_64-efi --efi-directory=/boot/efi --bootloader-id=rackauto --recheck && chroot /mnt/target grub-mkconfig -o /boot/grub/grub.cfg")
	}
	return runShell(log, "chroot /mnt/target grub-install --target=i386-pc --recheck "+disk+" && chroot /mnt/target grub-mkconfig -o /boot/grub/grub.cfg")
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
