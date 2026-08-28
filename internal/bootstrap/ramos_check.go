package bootstrap

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Songxwn/Rack-auto/internal/config"
)

// CheckRAMOS reports missing PXE RAMOS files machines need over HTTP.
func CheckRAMOS(cfg config.Config) error {
	arches := cfg.Bootstrap.UbuntuArches
	if len(arches) == 0 {
		arches = []string{"x86_64"}
	}
	var missing []string
	for _, arch := range arches {
		dir := filepath.Join(cfg.RAMOSDir(), "ubuntu", strings.TrimSpace(arch))
		for _, name := range []string{"vmlinuz", "initrd", "casper.iso", "layerfs-path"} {
			p := filepath.Join(dir, name)
			if st, err := os.Stat(p); err != nil || st.Size() == 0 {
				missing = append(missing, p)
			}
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("PXE RAMOS 未就绪（缺少 %s）；在控制面 Linux 上执行: rackauto bootstrap", strings.Join(missing, ", "))
}
