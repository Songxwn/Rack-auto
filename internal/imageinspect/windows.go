package imageinspect

import (
	"strings"
	"time"

	"github.com/Songxwn/Rack-auto/internal/model"
)

func isISO9660(r ioReaderAt0) bool {
	buf := make([]byte, 5)
	if _, err := r.ReadAt(buf, 16*2048+1); err != nil {
		return false
	}
	return string(buf) == "CD001"
}

type ioReaderAt0 interface {
	ReadAt(p []byte, off int64) (int, error)
}

func inspectWindowsISO(size int64, r ioReaderAt0) *model.ImageInspect {
	if !isISO9660(r) {
		return nil
	}
	exts := FindWIMExtents(r, size)
	if len(exts) == 0 {
		return nil
	}
	var boot, install *WIMExtent
	for i := range exts {
		e := &exts[i]
		if e.IsBootWIM() && boot == nil {
			cp := *e
			boot = &cp
			continue
		}
		if install == nil || e.Size > install.Size {
			cp := *e
			install = &cp
		}
	}
	if install == nil {
		install = &exts[len(exts)-1]
	}
	if boot != nil && install != nil && boot.Offset == install.Offset && len(exts) > 1 {
		for i := range exts {
			if exts[i].Offset != boot.Offset {
				cp := exts[i]
				install = &cp
				break
			}
		}
	}
	if boot == nil && install == nil {
		return nil
	}
	if boot == nil && install != nil && !hasServerEdition(install.Images) && !install.IsBootWIM() && len(exts) == 1 {
		// A random ISO that happens to contain one WIM is not necessarily Windows Setup media.
		if !hasWindowsSetupMarker(install.Images) {
			return nil
		}
	}
	out := &model.ImageInspect{
		Status:       "ok",
		Format:       "iso",
		Windows:      true,
		VirtualSizeB: size,
		InspectedAt:  time.Now().UTC(),
	}
	if install != nil {
		out.WIMImages = install.Images
		out.InstallOff = install.Offset
		out.InstallSize = install.Size
		out.InstallWIM = "install.wim"
	}
	if boot != nil {
		out.BootWIM = "boot.wim"
	}
	var warns []string
	if out.BootWIM == "" {
		warns = append(warns, "no WinPE boot.wim in ISO")
		out.Status = "warn"
	}
	if len(out.WIMImages) == 0 {
		warns = append(warns, "could not list install.wim editions")
		if out.Status != "error" {
			out.Status = "warn"
		}
	}
	if !hasServerEdition(out.WIMImages) && len(out.WIMImages) > 0 {
		warns = append(warns, "no Windows Server edition found (2019-2025 expected)")
	}
	out.Warnings = warns
	out.Message = wimSummary(out.WIMImages, false)
	if ver := DetectWindowsVersion(out.WIMImages); ver != "" {
		out.Message += "; Windows Server " + ver
	}
	return out
}

func hasServerEdition(images []model.WIMImage) bool {
	for _, im := range images {
		n := strings.ToLower(im.Name + " " + im.Description + " " + im.Flags + " " + im.Edition)
		if strings.Contains(n, "server") {
			return true
		}
	}
	return false
}

func hasWindowsSetupMarker(images []model.WIMImage) bool {
	for _, im := range images {
		n := strings.ToLower(im.Name + " " + im.Description + " " + im.Flags + " " + im.Edition)
		if strings.Contains(n, "windows") || strings.Contains(n, "server") || strings.Contains(n, "winpe") {
			return true
		}
	}
	return false
}
