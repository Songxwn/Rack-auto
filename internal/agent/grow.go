package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Songxwn/Rack-auto/internal/diskgrow"
	"github.com/Songxwn/Rack-auto/internal/provision"
)

func growDisk(log func(string, ...any), disk, rootDev string, hint int) error {
	parts := collectGrowParts(disk)
	if len(parts) == 0 {
		return fmt.Errorf("no partitions to grow")
	}
	diskSize := blockSize(disk)
	rootNum := hint
	if rootNum <= 0 {
		rootNum = diskgrow.PartNumFromPath(rootDev)
	}
	plan := diskgrow.PlanGrow(parts, diskSize, rootNum)
	if plan.Empty() {
		log("root already fills the disk")
		return nil
	}
	log("grow plan root=%d esp_move=%v grow=%v", rootNum, plan.MoveESP != nil, plan.Grow != nil)
	var espBackup string
	if plan.MoveESP != nil {
		esp := provision.PartitionPath(disk, plan.MoveESP.Num)
		espBackup = "/tmp/rackauto-esp.img"
		if err := run(log, "dd", "if="+esp, "of="+espBackup, "bs=1M", "conv=fsync", "status=none"); err != nil {
			return fmt.Errorf("backup ESP: %w", err)
		}
	}
	_ = run(log, "sgdisk", "-e", disk)
	dump, err := exec.Command("sfdisk", "-d", disk).Output()
	if err != nil {
		return fmt.Errorf("sfdisk dump: %w", err)
	}
	sectors := diskSize / 512
	script := diskgrow.RewriteSfdiskDump(string(dump), plan, sectors)
	cmd := exec.Command("sfdisk", "--no-reread", "--force", disk)
	cmd.Stdin = strings.NewReader(script)
	out, err := cmd.CombinedOutput()
	if len(out) > 0 {
		log("%s", strings.TrimSpace(string(out)))
	}
	if err != nil {
		return fmt.Errorf("sfdisk apply: %w", err)
	}
	_ = run(log, "partprobe", disk)
	if plan.MoveESP != nil && espBackup != "" {
		esp := provision.PartitionPath(disk, plan.MoveESP.Num)
		if err := run(log, "dd", "if="+espBackup, "of="+esp, "bs=1M", "conv=fsync", "status=none"); err != nil {
			return fmt.Errorf("restore ESP: %w", err)
		}
		_ = os.Remove(espBackup)
	}
	root := rootDev
	if plan.Grow != nil {
		root = provision.PartitionPath(disk, plan.Grow.Num)
	}
	fs := probeDevFS(root)
	switch fs {
	case "xfs":
		log("xfs will grow after mount")
	case "btrfs":
		log("btrfs will grow after mount")
	default:
		_ = run(log, "e2fsck", "-fp", root)
		if err := run(log, "resize2fs", root); err != nil {
			log("warn: resize2fs: %v", err)
		}
	}
	return nil
}

func collectGrowParts(disk string) []diskgrow.Part {
	var out []diskgrow.Part
	for _, p := range listDiskPartitions(disk) {
		n := diskgrow.PartNumFromPath(p)
		if n <= 0 {
			continue
		}
		start := sysInt(filepath.Join("/sys/class/block", filepath.Base(p), "start"))
		size := sysInt(filepath.Join("/sys/class/block", filepath.Base(p), "size"))
		if size <= 0 {
			continue
		}
		typ := "other"
		fs := probeDevFS(p)
		switch fs {
		case "vfat":
			typ = "esp"
		case "ext4", "xfs", "btrfs":
			typ = "linux"
		default:
			if size < 4096 {
				typ = "bios"
			}
		}
		out = append(out, diskgrow.Part{Num: n, StartB: start * 512, SizeB: size * 512, Type: typ})
	}
	return out
}

func sysInt(path string) int64 {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	n, _ := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	return n
}
