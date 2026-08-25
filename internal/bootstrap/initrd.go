package bootstrap

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
)

func appendInitrdOverlay(stock, dest, script string) error {
	extra, err := makeInitrdOverlay(script)
	if err != nil {
		return err
	}
	in, err := os.Open(stock)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dest + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if _, err := out.Write(extra); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dest)
}

func makeInitrdOverlay(script string) ([]byte, error) {
	files := []struct {
		name string
		body []byte
		mode uint32
	}{
		{name: "scripts/casper-bottom/99rackauto", body: []byte(script), mode: 0100755},
		{name: "conf/conf.d/zz-rackauto-stamp", body: []byte("rackauto\n"), mode: 0100644},
	}
	var raw bytes.Buffer
	for _, f := range files {
		if err := writeNewc(&raw, f.name, f.body, f.mode); err != nil {
			return nil, err
		}
	}
	if err := writeNewc(&raw, "TRAILER!!!", nil, 0); err != nil {
		return nil, err
	}
	for raw.Len()%4 != 0 {
		raw.WriteByte(0)
	}
	var gz bytes.Buffer
	w := gzip.NewWriter(&gz)
	if _, err := w.Write(raw.Bytes()); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return gz.Bytes(), nil
}

func writeNewc(w io.Writer, name string, body []byte, mode uint32) error {
	namesize := len(name) + 1
	hdr := fmt.Sprintf("%s%08x%08x%08x%08x%08x%08x%08x%08x%08x%08x%08x%08x%08x",
		"070701",
		0,
		mode,
		0, 0,
		1,
		0,
		len(body),
		0, 0, 0, 0,
		namesize,
		0,
	)
	if _, err := io.WriteString(w, hdr); err != nil {
		return err
	}
	if _, err := io.WriteString(w, name+"\x00"); err != nil {
		return err
	}
	pad := (4 - ((len(hdr) + namesize) % 4)) % 4
	if pad > 0 {
		if _, err := w.Write(make([]byte, pad)); err != nil {
			return err
		}
	}
	if _, err := w.Write(body); err != nil {
		return err
	}
	if len(body) > 0 {
		pad = (4 - (len(body) % 4)) % 4
		if pad > 0 {
			if _, err := w.Write(make([]byte, pad)); err != nil {
				return err
			}
		}
	}
	return nil
}

func casperBottomScript() string {
	return `#!/bin/sh
PREREQ=""
prereqs() { echo "$PREREQ"; }
case $1 in
prereqs) prereqs; exit 0 ;;
esac
[ -e /scripts/casper-functions ] && . /scripts/casper-functions
rootmnt="${rootmnt:-/root}"

SERVER=""
TOKEN=""
MAC=""
for x in $(cat /proc/cmdline); do
	case "$x" in
		rackauto_url=*) SERVER="${x#*=}" ;;
		rackauto_token=*) TOKEN="${x#*=}" ;;
		rackauto_mac=*) MAC="${x#*=}" ;;
	esac
done
[ -z "$SERVER" ] && exit 0

mkdir -p "${rootmnt}/usr/local/bin" "${rootmnt}/etc/systemd/system/multi-user.target.wants"
cat > "${rootmnt}/usr/local/bin/rackauto-boot.sh" << EOF
#!/bin/bash
trap 'sleep infinity' EXIT
export PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
SERVER='$SERVER'
TOKEN='$TOKEN'
MAC='$MAC'
mkdir -p /var/log /usr/local/bin
exec >>/var/log/rackauto.log 2>&1
echo "RAMOS boot \$(date -Is)"
i=0
while [ "\$i" -lt 60 ]; do
  curl -fsS "\${SERVER}/api/v1/health" >/dev/null && break
  i=\$((i+1)); sleep 2
done
ARCH=\$(uname -m)
case "\$ARCH" in aarch64|arm64) A=aarch64 ;; *) A=x86_64 ;; esac
curl -fL -o /usr/local/bin/rackauto-agent "\${SERVER}/boot/agent/\${A}/rackauto-agent" || \
  curl -fL -o /usr/local/bin/rackauto-agent "\${SERVER}/boot/agent/x86_64/rackauto-agent"
chmod +x /usr/local/bin/rackauto-agent
export DEBIAN_FRONTEND=noninteractive
apt-get update -qq >/dev/null 2>&1 || true
apt-get install -y -qq qemu-utils efibootmgr dosfstools e2fsprogs >/dev/null 2>&1 || true
/usr/local/bin/rackauto-agent --url "\$SERVER" --token "\$TOKEN" --mac "\$MAC" || true
sleep infinity
EOF
chmod 0755 "${rootmnt}/usr/local/bin/rackauto-boot.sh"
cat > "${rootmnt}/etc/systemd/system/rackauto-agent.service" << 'EOF'
[Unit]
Description=Rack-auto RAMOS agent
Wants=network-online.target
After=network-online.target
[Service]
Type=simple
ExecStart=/usr/local/bin/rackauto-boot.sh
Restart=always
RestartSec=5
[Install]
WantedBy=multi-user.target
EOF
ln -sf /etc/systemd/system/rackauto-agent.service "${rootmnt}/etc/systemd/system/multi-user.target.wants/rackauto-agent.service"
for s in subiquity.service snap.subiquity.service serial-subiquity.service plymouth-start.service; do
	ln -sf /dev/null "${rootmnt}/etc/systemd/system/${s}"
done
`
}
