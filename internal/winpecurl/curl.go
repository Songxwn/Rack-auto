// Package winpecurl is a tiny curl-compatible HTTP client for Windows PE.
// Official setup boot.wim has no curl.exe, certutil, or bitsadmin.
package winpecurl

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type options struct {
	fail       bool
	follow     bool
	output     string
	retries    int
	retryDelay time.Duration
	headers    [][2]string
	body       []byte
	post       bool
	url        string
}

func parse(args []string) (options, error) {
	var o options
	o.retryDelay = time.Second
	for i := 0; i < len(args); i++ {
		a := args[i]
		need := func() (string, error) {
			if i+1 >= len(args) {
				return "", fmt.Errorf("missing value for %s", a)
			}
			i++
			return args[i], nil
		}
		switch {
		case a == "-f" || a == "--fail":
			o.fail = true
		case a == "-L" || a == "--location":
			o.follow = true
		case a == "-s" || a == "--silent" || a == "-S" || a == "--show-error":
		case a == "-o" || a == "--output":
			v, err := need()
			if err != nil {
				return o, err
			}
			o.output = v
		case a == "-H" || a == "--header":
			v, err := need()
			if err != nil {
				return o, err
			}
			k, rest, ok := strings.Cut(v, ":")
			if !ok {
				return o, fmt.Errorf("bad header %q", v)
			}
			o.headers = append(o.headers, [2]string{strings.TrimSpace(k), strings.TrimSpace(rest)})
		case a == "-d" || a == "--data" || a == "--data-raw":
			v, err := need()
			if err != nil {
				return o, err
			}
			o.body = []byte(v)
			o.post = true
		case a == "--data-binary":
			v, err := need()
			if err != nil {
				return o, err
			}
			if strings.HasPrefix(v, "@") {
				b, err := os.ReadFile(strings.TrimPrefix(v, "@"))
				if err != nil {
					return o, err
				}
				o.body = b
			} else {
				o.body = []byte(v)
			}
			o.post = true
		case a == "--retry":
			v, err := need()
			if err != nil {
				return o, err
			}
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 {
				return o, fmt.Errorf("bad --retry")
			}
			o.retries = n
		case a == "--retry-delay":
			v, err := need()
			if err != nil {
				return o, err
			}
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 {
				return o, fmt.Errorf("bad --retry-delay")
			}
			o.retryDelay = time.Duration(n) * time.Second
		case strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--"):
			for _, c := range a[1:] {
				switch c {
				case 'f':
					o.fail = true
				case 'L':
					o.follow = true
				case 's', 'S':
				default:
					return o, fmt.Errorf("unknown flag -%c", c)
				}
			}
		case strings.HasPrefix(a, "-"):
			return o, fmt.Errorf("unknown flag %s", a)
		default:
			if o.url != "" {
				return o, fmt.Errorf("extra argument %s", a)
			}
			o.url = a
		}
	}
	if o.url == "" {
		return o, fmt.Errorf("url required")
	}
	return o, nil
}

// Main runs a curl-compatible client. Exit codes follow curl (22 = HTTP error with -f).
func Main(args []string) int {
	o, err := parse(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "winpe-curl:", err)
		return 2
	}
	var last error
	var lastCode int
	tries := o.retries + 1
	for n := 0; n < tries; n++ {
		if n > 0 {
			time.Sleep(o.retryDelay)
		}
		code, err := doOnce(o)
		if err == nil {
			return code
		}
		last = err
		lastCode = code
		fmt.Fprintln(os.Stderr, "winpe-curl:", err)
	}
	if last != nil {
		if lastCode != 0 {
			return lastCode
		}
		return 1
	}
	return 0
}

func doOnce(o options) (int, error) {
	method := http.MethodGet
	var body io.Reader
	if o.post {
		method = http.MethodPost
		body = bytes.NewReader(o.body)
	}
	req, err := http.NewRequest(method, o.url, body)
	if err != nil {
		return 1, err
	}
	req.Header.Set("User-Agent", "curl/8.0.0-rackauto")
	for _, h := range o.headers {
		req.Header.Set(h[0], h[1])
	}
	client := &http.Client{Timeout: 3 * time.Hour}
	if !o.follow {
		client.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return 1, err
	}
	defer resp.Body.Close()
	if o.fail && resp.StatusCode >= 400 {
		_, _ = io.Copy(io.Discard, resp.Body)
		return 22, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if o.output != "" {
		tmp := o.output + ".tmp"
		f, err := os.Create(tmp)
		if err != nil {
			return 1, err
		}
		keep := false
		defer func() {
			_ = f.Close()
			if !keep {
				_ = os.Remove(tmp)
			}
		}()
		pw := &countWriter{w: f}
		if _, err := io.Copy(pw, resp.Body); err != nil {
			return 1, err
		}
		if err := f.Close(); err != nil {
			return 1, err
		}
		if resp.ContentLength >= 0 && pw.n != resp.ContentLength {
			return 1, fmt.Errorf("short read %d bytes, Content-Length %d", pw.n, resp.ContentLength)
		}
		_ = os.Remove(o.output)
		if err := os.Rename(tmp, o.output); err != nil {
			return 1, err
		}
		if wimOutput(o.output) && !fileHasWIMMagic(o.output) {
			_ = os.Remove(o.output)
			return 1, fmt.Errorf("downloaded file is not a WIM (HTML/truncated)")
		}
		fmt.Fprintf(os.Stderr, "winpe-curl: wrote %d bytes\n", pw.n)
		keep = true
		return 0, nil
	}
	if _, err := io.Copy(os.Stdout, resp.Body); err != nil {
		return 1, err
	}
	return 0, nil
}

type countWriter struct {
	w    io.Writer
	n    int64
	last int64
}

func (c *countWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	if c.n-c.last >= 64<<20 {
		fmt.Fprintf(os.Stderr, "winpe-curl: %d MB\n", c.n>>20)
		c.last = c.n
	}
	return n, err
}

func wimOutput(path string) bool {
	s := strings.ToLower(path)
	return strings.HasSuffix(s, ".wim") || strings.HasSuffix(s, ".esd")
}

func fileHasWIMMagic(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var m [8]byte
	if _, err := io.ReadFull(f, m[:]); err != nil {
		return false
	}
	return string(m[:]) == "MSWIM\x00\x00\x00"
}
