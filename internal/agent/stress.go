package agent

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/Songxwn/Rack-auto/internal/model"
)

func (c *Client) RunStress(ctx context.Context, job *model.AgentJob) (*model.StressResult, error) {
	spec, err := DecodeJSON[model.StressSpec](job.Params)
	if err != nil {
		return nil, err
	}
	if spec.DurationSec <= 0 {
		spec.DurationSec = 60
	}
	res := &model.StressResult{}
	want := map[string]bool{}
	for _, t := range spec.Targets {
		want[t] = true
	}
	log := func(format string, a ...any) {
		line := fmt.Sprintf(format, a...)
		fmt.Println(line)
		c.Log(job.ID, line)
	}
	if want["cpu"] {
		c.Progress(job.ID, 10, "CPU 压测中")
		r := stressCPU(ctx, spec)
		res.CPU = &r
		log("CPU: %d workers, %.0f hash/s", r.Workers, r.HashRate)
	}
	if want["memory"] {
		c.Progress(job.ID, 35, "内存压测中")
		r := stressMemory(ctx, spec)
		res.Memory = &r
		log("内存: %d MB, %d errors, %.1f MB/s", r.Bytes>>20, r.Errors, r.Throughput)
	}
	if want["disk"] {
		c.Progress(job.ID, 60, "磁盘压测中")
		r, err := stressDisk(ctx, spec)
		if err != nil {
			return res, err
		}
		res.Disk = &r
		log("磁盘: write %.1f MB/s read %.1f MB/s verify=%v", r.WriteMBs, r.ReadMBs, r.VerifyOK)
	}
	if want["network"] {
		c.Progress(job.ID, 80, "网络压测中")
		r, err := stressNetwork(ctx, c, spec)
		if err != nil {
			return res, err
		}
		res.Network = &r
		log("网络: down %.1f MB/s up %.1f MB/s", r.DownloadMBs, r.UploadMBs)
	}
	c.Progress(job.ID, 100, "压测完成")
	return res, nil
}

func stressCPU(ctx context.Context, spec model.StressSpec) model.CPUResult {
	n := spec.CPUWorkers
	if n <= 0 {
		n = runtime.NumCPU()
	}
	d := time.Duration(spec.DurationSec) * time.Second
	ctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	var hashes atomic.Uint64
	start := time.Now()
	done := make(chan struct{})
	for i := 0; i < n; i++ {
		go func() {
			buf := make([]byte, 64)
			for {
				select {
				case <-ctx.Done():
					return
				default:
					sum := sha256.Sum256(buf)
					copy(buf, sum[:])
					hashes.Add(1)
				}
			}
		}()
	}
	<-ctx.Done()
	close(done)
	sec := time.Since(start).Seconds()
	h := hashes.Load()
	return model.CPUResult{Workers: n, Hashes: h, Seconds: sec, HashRate: float64(h) / sec}
}

func stressMemory(ctx context.Context, spec model.StressSpec) model.MemoryResult {
	pct := spec.MemoryPercent
	if pct <= 0 {
		pct = 50
	}
	if pct > 90 {
		pct = 90
	}
	total := int64(CollectInventory().MemoryMB) << 20
	if total <= 0 {
		total = 1 << 30
	}
	size := total * int64(pct) / 100
	if size < 64<<20 {
		size = 64 << 20
	}
	buf := make([]byte, size)
	start := time.Now()
	pattern := byte(0xA5)
	for i := range buf {
		buf[i] = pattern
	}
	d := time.Duration(spec.DurationSec) * time.Second
	deadline := time.Now().Add(d)
	errors := 0
	passes := 0
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			deadline = time.Now()
		default:
		}
		pattern++
		for i := range buf {
			buf[i] = pattern
		}
		for i := range buf {
			if buf[i] != pattern {
				errors++
			}
		}
		passes++
	}
	sec := time.Since(start).Seconds()
	_ = buf[0]
	mb := float64(size*int64(passes)) / 1e6 / sec
	return model.MemoryResult{Bytes: size, Seconds: sec, Errors: errors, Throughput: mb}
}

func stressDisk(ctx context.Context, spec model.StressSpec) (model.DiskResult, error) {
	sizeMB := spec.DiskSizeMB
	if sizeMB <= 0 {
		sizeMB = 512
	}
	path := spec.DiskPath
	if path == "" {
		path = filepath.Join(os.TempDir(), "rackauto-disk-stress.bin")
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	f, err := os.Create(path)
	if err != nil {
		return model.DiskResult{}, err
	}
	defer func() { _ = os.Remove(path) }()
	chunk := make([]byte, 1<<20)
	_, _ = rand.Read(chunk)
	start := time.Now()
	var written int64
	for i := 0; i < sizeMB; i++ {
		select {
		case <-ctx.Done():
			_ = f.Close()
			return model.DiskResult{}, ctx.Err()
		default:
		}
		n, err := f.Write(chunk)
		written += int64(n)
		if err != nil {
			_ = f.Close()
			return model.DiskResult{}, err
		}
	}
	_ = f.Sync()
	writeSec := time.Since(start).Seconds()
	_, _ = f.Seek(0, 0)
	start = time.Now()
	h := sha256.New()
	read, err := io.Copy(h, f)
	_ = f.Close()
	if err != nil {
		return model.DiskResult{}, err
	}
	readSec := time.Since(start).Seconds()
	ok := read == written
	return model.DiskResult{
		Path: path, SizeMB: sizeMB, VerifyOK: ok,
		WriteMBs: float64(written) / 1e6 / writeSec,
		ReadMBs:  float64(read) / 1e6 / readSec,
		Seconds:  writeSec + readSec,
	}, nil
}

func stressNetwork(ctx context.Context, c *Client, spec model.StressSpec) (model.NetworkResult, error) {
	mb := 64
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/api/v1/agent/speedtest?mb=%d", c.URL, mb), nil)
	if err != nil {
		return model.NetworkResult{}, err
	}
	if c.Token != "" {
		req.Header.Set("X-API-Token", c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return model.NetworkResult{}, err
	}
	n, err := io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if err != nil {
		return model.NetworkResult{}, err
	}
	downSec := time.Since(start).Seconds()
	start = time.Now()
	buf := make([]byte, mb<<20)
	req, err = http.NewRequestWithContext(ctx, http.MethodPost, c.URL+"/api/v1/agent/speedtest", bytesReader(buf))
	if err != nil {
		return model.NetworkResult{}, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	if c.Token != "" {
		req.Header.Set("X-API-Token", c.Token)
	}
	resp, err = c.HTTP.Do(req)
	if err != nil {
		return model.NetworkResult{}, err
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	upSec := time.Since(start).Seconds()
	_ = spec
	return model.NetworkResult{
		DownloadMBs: float64(n) / 1e6 / downSec,
		UploadMBs:   float64(len(buf)) / 1e6 / upSec,
		Seconds:     downSec + upSec,
	}, nil
}

func bytesReader(b []byte) io.Reader { return &memReader{b: b} }

type memReader struct {
	b []byte
	i int
}

func (m *memReader) Read(p []byte) (int, error) {
	if m.i >= len(m.b) {
		return 0, io.EOF
	}
	n := copy(p, m.b[m.i:])
	m.i += n
	return n, nil
}
