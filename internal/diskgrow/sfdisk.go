package diskgrow

import (
	"fmt"
	"strconv"
	"strings"
)

func RewriteSfdiskDump(dump string, plan Plan, diskSectors int64) string {
	var b strings.Builder
	for _, line := range strings.Split(dump, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "last-lba:") && diskSectors > 34 {
			fmt.Fprintf(&b, "last-lba: %d\n", diskSectors-34)
			continue
		}
		dev, start, size, rest, ok := parseSfdiskPart(trim)
		if !ok {
			b.WriteString(line)
			b.WriteByte('\n')
			continue
		}
		num := PartNumFromPath(dev)
		if plan.MoveESP != nil && num == plan.MoveESP.Num {
			start = plan.MoveESP.NewStartB / sector
			size = plan.MoveESP.SizeB / sector
		}
		if plan.Grow != nil && num == plan.Grow.Num {
			start = plan.Grow.StartB / sector
			size = plan.Grow.NewSize / sector
		}
		fmt.Fprintf(&b, "%s : start= %d, size= %d%s\n", dev, start, size, rest)
	}
	out := b.String()
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return out
}

func parseSfdiskPart(line string) (dev string, start, size int64, rest string, ok bool) {
	i := strings.Index(line, " : ")
	if i < 0 {
		return "", 0, 0, "", false
	}
	dev = strings.TrimSpace(line[:i])
	if !strings.HasPrefix(dev, "/dev/") {
		return "", 0, 0, "", false
	}
	fields := line[i+3:]
	start = fieldInt(fields, "start=")
	size = fieldInt(fields, "size=")
	if size == 0 && start == 0 {
		return "", 0, 0, "", false
	}
	rest = fields
	if j := strings.Index(rest, "type="); j >= 0 {
		// keep from the comma before type, or from type=
		k := strings.LastIndex(rest[:j], ",")
		if k >= 0 {
			rest = rest[k:]
		} else {
			rest = ", " + strings.TrimSpace(rest[j:])
		}
	} else {
		rest = ""
	}
	return dev, start, size, rest, true
}

func fieldInt(s, key string) int64 {
	i := strings.Index(s, key)
	if i < 0 {
		return 0
	}
	s = strings.TrimSpace(s[i+len(key):])
	n := 0
	for n < len(s) && s[n] >= '0' && s[n] <= '9' {
		n++
	}
	v, _ := strconv.ParseInt(s[:n], 10, 64)
	return v
}

func PartNumFromPath(p string) int {
	base := p
	if i := strings.LastIndex(p, "/"); i >= 0 {
		base = p[i+1:]
	}
	if i := strings.LastIndex(base, "p"); i > 0 {
		if n, err := strconv.Atoi(base[i+1:]); err == nil && n > 0 {
			return n
		}
	}
	i := len(base)
	for i > 0 && base[i-1] >= '0' && base[i-1] <= '9' {
		i--
	}
	n, _ := strconv.Atoi(base[i:])
	return n
}
