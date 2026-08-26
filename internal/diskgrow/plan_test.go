package diskgrow

import (
	"strings"
	"testing"
)

func TestPlanGrowRootLast(t *testing.T) {
	parts := []Part{
		{Num: 1, StartB: 2048 * 512, SizeB: 2 << 30, Type: "linux"},
	}
	p := PlanGrow(parts, 20<<30, 1)
	if p.Grow == nil || p.Grow.Num != 1 || p.Grow.NewSize <= 2<<30 {
		t.Fatalf("%+v", p)
	}
}

func TestPlanGrowUbuntuESPAtEnd(t *testing.T) {
	const img = 3 << 30
	esp := int64(100 << 20)
	rootStart := int64(2048 * 512)
	espStart := img - 34*512 - esp
	rootSize := espStart - rootStart
	parts := []Part{
		{Num: 14, StartB: 34 * 512, SizeB: 2014 * 512, Type: "bios"},
		{Num: 1, StartB: rootStart, SizeB: rootSize, Type: "linux"},
		{Num: 15, StartB: espStart, SizeB: esp, Type: "esp"},
	}
	p := PlanGrow(parts, 50<<30, 1)
	if p.MoveESP == nil || p.MoveESP.Num != 15 {
		t.Fatalf("move esp %+v", p)
	}
	if p.Grow == nil || p.Grow.Num != 1 {
		t.Fatalf("grow %+v", p)
	}
	if p.Grow.NewSize <= rootSize {
		t.Fatalf("root not grown %+v", p)
	}
	if p.MoveESP.NewStartB <= espStart {
		t.Fatalf("esp not moved %+v", p)
	}
}

func TestPlanGrowNoSpace(t *testing.T) {
	parts := []Part{{Num: 1, StartB: 2048 * 512, SizeB: 10<<30 - 2048*512 - 34*512, Type: "linux"}}
	p := PlanGrow(parts, 10<<30, 1)
	if !p.Empty() && p.Grow != nil && p.Grow.NewSize > parts[0].SizeB+1<<20 {
		t.Fatalf("should not grow much %+v", p)
	}
}

func TestRewriteSfdiskDump(t *testing.T) {
	dump := `label: gpt
label-id: 1234
device: /dev/sda
unit: sectors
first-lba: 34
last-lba: 6291422
sector-size: 512

/dev/sda1 : start= 2048, size= 4194304, type=0FC63DAF-8483-4772-8E79-3D69D8477DE4
/dev/sda15 : start= 6094848, size= 204800, type=C12A7328-F81F-11D2-BA4B-00A0C93EC93B
`
	plan := Plan{
		Grow:    &Grow{Num: 1, StartB: 2048 * 512, NewSize: 10 << 20},
		MoveESP: &Move{Num: 15, NewStartB: 20 << 20, SizeB: 204800 * 512},
	}
	out := RewriteSfdiskDump(dump, plan, 50<<30/512)
	if !strings.Contains(out, "last-lba: ") {
		t.Fatal(out)
	}
	if !strings.Contains(out, "/dev/sda1 : start= 2048, size= 20480") {
		t.Fatal(out)
	}
	if !strings.Contains(out, "/dev/sda15 : start= 40960, size= 204800") {
		t.Fatal(out)
	}
}

func TestPartNumFromPath(t *testing.T) {
	if PartNumFromPath("/dev/sda1") != 1 || PartNumFromPath("/dev/sda15") != 15 {
		t.Fatal("sda")
	}
	if PartNumFromPath("/dev/nvme0n1p15") != 15 {
		t.Fatal("nvme")
	}
}
