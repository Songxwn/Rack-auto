package diskgrow

const sector = 512
const gptTail = 34 * sector
const align = 1 << 20

type Part struct {
	Num    int
	StartB int64
	SizeB  int64
	Type   string
}

type Move struct {
	Num       int
	NewStartB int64
	SizeB     int64
}

type Grow struct {
	Num     int
	StartB  int64
	NewSize int64
}

type Plan struct {
	RelocateGPT bool
	MoveESP     *Move
	Grow        *Grow
}

func (p Plan) Empty() bool {
	return p.MoveESP == nil && p.Grow == nil
}

func PlanGrow(parts []Part, diskSize int64, rootNum int) Plan {
	if diskSize < align*2 || len(parts) == 0 {
		return Plan{}
	}
	root := pickRoot(parts, rootNum)
	if root.Num == 0 || root.SizeB <= 0 {
		return Plan{}
	}
	endUsed := int64(0)
	var last Part
	var esp Part
	for _, p := range parts {
		e := p.StartB + p.SizeB
		if e > endUsed {
			endUsed = e
			last = p
		}
		if p.Type == "esp" {
			esp = p
		}
	}
	lastUsable := diskSize - gptTail
	if lastUsable-endUsed < align {
		return Plan{}
	}
	out := Plan{RelocateGPT: true}
	if esp.Num != 0 && last.Num == esp.Num && root.Num != esp.Num && root.StartB < esp.StartB {
		newESP := alignDown(lastUsable-esp.SizeB, align)
		if newESP > esp.StartB+align {
			out.MoveESP = &Move{Num: esp.Num, NewStartB: newESP, SizeB: esp.SizeB}
			newRoot := newESP - root.StartB
			if newRoot > root.SizeB+align {
				out.Grow = &Grow{Num: root.Num, StartB: root.StartB, NewSize: newRoot}
			}
			return out
		}
	}
	if last.Num == root.Num {
		newSize := lastUsable - root.StartB
		newSize = alignDown(newSize, align)
		if newSize > root.SizeB+align {
			out.Grow = &Grow{Num: root.Num, StartB: root.StartB, NewSize: newSize}
		}
	}
	return out
}

func pickRoot(parts []Part, hint int) Part {
	if hint > 0 {
		for _, p := range parts {
			if p.Num == hint && p.Type != "esp" && p.Type != "bios" {
				return p
			}
		}
	}
	var best Part
	for _, p := range parts {
		if p.Type == "esp" || p.Type == "bios" {
			continue
		}
		if p.SizeB >= best.SizeB {
			best = p
		}
	}
	return best
}

func alignDown(n, a int64) int64 {
	if a <= 0 {
		return n
	}
	return n - n%a
}
