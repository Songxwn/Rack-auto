package agent

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	qcow2reader "github.com/lima-vm/go-qcow2reader"
	"github.com/lima-vm/go-qcow2reader/image/qcow2"
)

func looksQcow(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return strings.Contains(path, "qcow")
	}
	defer f.Close()
	hdr := make([]byte, 4)
	_, _ = f.Read(hdr)
	return string(hdr) == "QFI\xfb"
}

// writeDiskImage copies src onto dest (file or block device). qcow2 is decoded
// without qemu-img so RAMOS does not wait on apt.
func writeDiskImage(log func(string, ...any), src, dest string, progress func(copied, total int64)) error {
	if looksQcow(src) {
		if _, err := exec.LookPath("qemu-img"); err == nil {
			return run(log, "qemu-img", "convert", "-p", "-O", "raw", src, dest)
		}
		log("qemu-img 不在 PATH，使用内置 qcow2 转换 → %s", dest)
		return convertQcowTo(src, dest, progress)
	}
	return run(log, "dd", "if="+src, "of="+dest, "bs=8M", "conv=fsync", "status=progress")
}

func convertQcowTo(src, dest string, progress func(copied, total int64)) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	img, err := qcow2reader.OpenWithType(in, qcow2.Type)
	if err != nil {
		return fmt.Errorf("打开 qcow2: %w", err)
	}
	defer img.Close()
	if err := img.Readable(); err != nil {
		return fmt.Errorf("qcow2 无法读取: %w", err)
	}
	size := img.Size()
	if size <= 0 {
		return fmt.Errorf("qcow2 虚拟大小无效")
	}
	out, err := os.OpenFile(dest, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	if st, err := out.Stat(); err == nil && st.Mode().IsRegular() {
		if err := out.Truncate(size); err != nil {
			return err
		}
	}
	buf := make([]byte, 8<<20)
	r := io.NewSectionReader(img, 0, size)
	var copied int64
	for {
		n, err := r.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				return werr
			}
			copied += int64(n)
			if progress != nil {
				progress(copied, size)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	if copied != size {
		return fmt.Errorf("qcow2 写入不完整: %d / %d", copied, size)
	}
	return out.Sync()
}
