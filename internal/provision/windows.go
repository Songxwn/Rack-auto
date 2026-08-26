package provision

import (
	"encoding/json"
	"fmt"
	"html"
	"net"
	"strconv"
	"strings"

	"github.com/Songxwn/Rack-auto/internal/model"
)

func DefaultWIMIndex(images []model.WIMImage) int {
	if len(images) == 0 {
		return 1
	}
	score := func(im model.WIMImage) int {
		n := strings.ToUpper(im.Name + " " + im.Description + " " + im.Flags + " " + im.Edition)
		s := 0
		if strings.Contains(n, "SERVER") {
			s += 10
		}
		if strings.Contains(n, "STANDARD") {
			s += 5
		}
		if strings.Contains(n, "DATACENTER") {
			s += 3
		}
		if strings.Contains(n, "CORE") {
			s -= 4
		}
		if strings.Contains(n, "HYPER-V") || strings.Contains(n, "HYPERV") {
			s -= 8
		}
		return s
	}
	best := images[0]
	bestS := score(best)
	for _, im := range images[1:] {
		if s := score(im); s > bestS {
			best, bestS = im, s
		}
	}
	if best.Index > 0 {
		return best.Index
	}
	return 1
}

func WindowsHostname(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	for _, r := range s {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 15 {
		out = strings.TrimRight(out[:15], "-")
	}
	if out == "" {
		return "WIN-HOST"
	}
	return out
}

func WindowsTimeZone(tz string) string {
	switch strings.TrimSpace(tz) {
	case "Asia/Shanghai", "Asia/Chongqing", "Asia/Harbin", "Asia/Urumqi":
		return "China Standard Time"
	case "Asia/Hong_Kong":
		return "China Standard Time"
	case "Asia/Singapore":
		return "Singapore Standard Time"
	case "Asia/Tokyo":
		return "Tokyo Standard Time"
	case "Asia/Seoul":
		return "Korea Standard Time"
	case "UTC", "Etc/UTC", "Etc/GMT":
		return "UTC"
	case "Europe/London":
		return "GMT Standard Time"
	case "Europe/Berlin":
		return "W. Europe Standard Time"
	case "America/New_York":
		return "Eastern Standard Time"
	case "America/Los_Angeles":
		return "Pacific Standard Time"
	case "Australia/Sydney":
		return "AUS Eastern Standard Time"
	default:
		if strings.Contains(strings.ToLower(tz), "standard time") {
			return tz
		}
		return "China Standard Time"
	}
}

func WinPEDiskNumber(disk string) int {
	disk = strings.TrimSpace(strings.ToLower(disk))
	if disk == "" || disk == "auto" {
		return 0
	}
	if strings.HasPrefix(disk, "/dev/nvme") {
		var n int
		fmt.Sscanf(disk, "/dev/nvme%d", &n)
		if n >= 0 {
			return n
		}
	}
	if (strings.HasPrefix(disk, "/dev/sd") || strings.HasPrefix(disk, "/dev/vd") || strings.HasPrefix(disk, "/dev/hd") || strings.HasPrefix(disk, "/dev/xvd")) && len(disk) >= 8 {
		c := disk[len(disk)-1]
		if c >= 'a' && c <= 'z' {
			return int(c - 'a')
		}
	}
	if strings.HasPrefix(disk, "disk") {
		n, err := strconv.Atoi(strings.TrimPrefix(disk, "disk"))
		if err == nil && n >= 0 {
			return n
		}
	}
	if n, err := strconv.Atoi(disk); err == nil && n >= 0 {
		return n
	}
	return 0
}

func DiskpartScript(firmware string, diskNum int) string {
	if diskNum < 0 {
		diskNum = 0
	}
	if strings.EqualFold(firmware, model.FirmwareBIOS) {
		return fmt.Sprintf("select disk %d\nclean\nconvert mbr\ncreate partition primary size=100\nformat quick fs=ntfs label=System\nassign letter=S\nactive\ncreate partition primary\nformat quick fs=ntfs label=Windows\nassign letter=W\nexit\n", diskNum)
	}
	return fmt.Sprintf("select disk %d\nclean\nconvert gpt\ncreate partition efi size=100\nformat quick fs=fat32 label=System\nassign letter=S\ncreate partition msr size=16\ncreate partition primary\nformat quick fs=ntfs label=Windows\nassign letter=W\nexit\n", diskNum)
}

func StartnetCMD() string {
	return "@echo off\r\nwpeinit\r\nif exist X:\\install.cmd call X:\\install.cmd\r\nif exist install.cmd call install.cmd\r\n"
}

func WinpeshlINI() string {
	return "[LaunchApps]\r\n%SYSTEMDRIVE%\\Windows\\System32\\wpeinit.exe\r\nX:\\install.cmd\r\n"
}

type WindowsMedia struct {
	Startnet     string
	Winpeshl     string
	Diskpart     string
	Install      string
	Unattend     string
	CompleteJSON string
	FailJSON     string
}

func WindowsJobMedia(base, token, jobID, mac string, spec model.InstallSpec, img model.Image) WindowsMedia {
	base = strings.TrimRight(base, "/")
	idx := spec.WIMIndex
	if idx <= 0 && img.Inspect != nil {
		idx = DefaultWIMIndex(img.Inspect.WIMImages)
	}
	if idx <= 0 {
		idx = 1
	}
	installName := "install.wim"
	if img.Inspect != nil && img.Inspect.InstallWIM != "" {
		installName = img.Inspect.InstallWIM
		if i := strings.LastIndex(installName, "/"); i >= 0 {
			installName = installName[i+1:]
		}
	}
	wimURL := fmt.Sprintf("%s/images/win/%s/%s", base, img.ID, installName)
	unattendURL := fmt.Sprintf("%s/ipxe/windows/%s/unattend.xml", base, mac)
	progressURL := fmt.Sprintf("%s/api/v1/agent/jobs/%s/progress", base, jobID)
	completeURL := fmt.Sprintf("%s/api/v1/agent/jobs/%s/complete", base, jobID)
	bcd := "UEFI"
	if strings.EqualFold(spec.Firmware, model.FirmwareBIOS) {
		bcd = "BIOS"
	}
	okBody, _ := json.Marshal(map[string]any{"ok": true, "message": "Windows image applied"})
	failBody, _ := json.Marshal(map[string]any{"ok": false, "message": "Windows PE install failed"})
	return WindowsMedia{
		Startnet:     StartnetCMD(),
		Winpeshl:     WinpeshlINI(),
		Diskpart:     DiskpartScript(spec.Firmware, WinPEDiskNumber(spec.Disk)),
		Install:      windowsInstallCMD(token, wimURL, unattendURL, progressURL, completeURL, idx, bcd, spec.Reboot),
		Unattend:     UnattendXML(spec, mac),
		CompleteJSON: string(okBody) + "\n",
		FailJSON:     string(failBody) + "\n",
	}
}

func windowsInstallCMD(token, wimURL, unattendURL, progressURL, completeURL string, index int, bcd string, reboot bool) string {
	tok := batEscape(token)
	var b strings.Builder
	b.WriteString("@echo off\r\nsetlocal EnableExtensions\r\n")
	fmt.Fprintf(&b, "set TOKEN=%s\r\n", tok)
	fmt.Fprintf(&b, "set WIMURL=%s\r\n", batEscape(wimURL))
	fmt.Fprintf(&b, "set UNATTEND=%s\r\n", batEscape(unattendURL))
	fmt.Fprintf(&b, "set PROGRESS=%s\r\n", batEscape(progressURL))
	fmt.Fprintf(&b, "set COMPLETE=%s\r\n", batEscape(completeURL))
	b.WriteString("cd /d X:\\\r\n")
	b.WriteString("call :report 8 partitioning\r\n")
	b.WriteString("diskpart /s X:\\diskpart.txt\r\n")
	b.WriteString("if errorlevel 1 goto fail\r\n")
	b.WriteString("if not exist W:\\ nul goto fail\r\n")
	b.WriteString("call :report 20 downloading_wim\r\n")
	b.WriteString("curl.exe -fL --retry 8 --retry-delay 5 -o W:\\install.wim \"%WIMURL%\"\r\n")
	b.WriteString("if errorlevel 1 goto fail\r\n")
	b.WriteString("call :report 45 applying_image\r\n")
	fmt.Fprintf(&b, "dism.exe /Apply-Image /ImageFile:W:\\install.wim /Index:%d /ApplyDir:W:\\\r\n", index)
	b.WriteString("if errorlevel 1 goto fail\r\n")
	b.WriteString("call :report 80 bootloader\r\n")
	fmt.Fprintf(&b, "bcdboot W:\\Windows /s S: /f %s\r\n", bcd)
	b.WriteString("if errorlevel 1 (\r\n")
	b.WriteString("  bcdboot W:\\Windows /s S: /f BIOS\r\n")
	b.WriteString("  if errorlevel 1 bcdboot W:\\Windows /s S: /f UEFI\r\n")
	b.WriteString(")\r\n")
	b.WriteString("mkdir W:\\Windows\\Panther >nul 2>nul\r\n")
	b.WriteString("curl.exe -fL --retry 8 --retry-delay 2 -o W:\\Windows\\Panther\\unattend.xml \"%UNATTEND%\"\r\n")
	b.WriteString("if exist W:\\install.wim del /f /q W:\\install.wim\r\n")
	b.WriteString("call :report 95 applied\r\n")
	b.WriteString("curl.exe -fL --retry 5 --retry-delay 2 -H \"X-API-Token: %TOKEN%\" -H \"Content-Type: application/json\" --data-binary @X:\\complete.json \"%COMPLETE%\"\r\n")
	if reboot {
		b.WriteString("wpeutil reboot\r\n")
	}
	b.WriteString("goto :eof\r\n")
	b.WriteString(":report\r\n")
	b.WriteString(">X:\\p.json echo {\"progress\":%1,\"message\":\"%~2\"}\r\n")
	b.WriteString("curl.exe -fL -H \"X-API-Token: %TOKEN%\" -H \"Content-Type: application/json\" --data-binary @X:\\p.json \"%PROGRESS%\" >nul 2>nul\r\n")
	b.WriteString("echo %~2\r\n")
	b.WriteString("exit /b 0\r\n")
	b.WriteString(":fail\r\n")
	b.WriteString("echo INSTALL FAILED\r\n")
	b.WriteString("curl.exe -fL -H \"X-API-Token: %TOKEN%\" -H \"Content-Type: application/json\" --data-binary @X:\\fail.json \"%COMPLETE%\" >nul 2>nul\r\n")
	b.WriteString("pause\r\n")
	b.WriteString("exit /b 1\r\n")
	return b.String()
}

func batEscape(s string) string {
	s = strings.ReplaceAll(s, "%", "%%")
	return s
}

func UnattendXML(spec model.InstallSpec, pxeMAC string) string {
	user := strings.TrimSpace(spec.Username)
	if user == "" {
		user = "Administrator"
	}
	host := WindowsHostname(spec.Hostname)
	tz := WindowsTimeZone(spec.Timezone)
	pass := xmlEscape(spec.Password)
	userEsc := xmlEscape(user)
	hostEsc := xmlEscape(host)
	org := "Rack-auto"
	key := strings.TrimSpace(spec.ProductKey)
	keyXML := "<ProductKey><WillShowUI>OnError</WillShowUI></ProductKey>"
	if key != "" {
		keyXML = "<ProductKey><Key>" + xmlEscape(key) + "</Key><WillShowUI>OnError</WillShowUI></ProductKey>"
	}
	localAccount := ""
	autoUser := "Administrator"
	if !strings.EqualFold(user, "Administrator") {
		autoUser = user
		localAccount = fmt.Sprintf(`
        <LocalAccounts>
          <LocalAccount wcm:action="add">
            <Name>%s</Name>
            <Group>Administrators</Group>
            <Password>
              <Value>%s</Value>
              <PlainText>true</PlainText>
            </Password>
          </LocalAccount>
        </LocalAccounts>`, userEsc, pass)
	}
	rdp := ""
	if spec.EnableRDP {
		rdp = `
    <component name="Microsoft-Windows-TerminalServices-LocalSessionManager" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
      <fDenyTSConnections>false</fDenyTSConnections>
    </component>
    <component name="Microsoft-Windows-TerminalServices-RDP-WinStationExtensions" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
      <UserAuthentication>0</UserAuthentication>
    </component>
    <component name="Networking-MPSSVC-Svc" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
      <FirewallGroups>
        <FirewallGroup wcm:action="add" wcm:keyValue="RemoteDesktop">
          <Active>true</Active>
          <Group>@FirewallAPI.dll,-28752</Group>
          <Profile>all</Profile>
        </FirewallGroup>
      </FirewallGroups>
    </component>`
	}
	tcp, dns := windowsNetworkXML(spec.Network, pxeMAC)
	return `<?xml version="1.0" encoding="utf-8"?>
<unattend xmlns="urn:schemas-microsoft-com:unattend">
  <settings pass="windowsPE">
    <component name="Microsoft-Windows-International-Core-WinPE" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
      <SetupUILanguage>
        <UILanguage>en-US</UILanguage>
      </SetupUILanguage>
      <InputLocale>en-US</InputLocale>
      <SystemLocale>en-US</SystemLocale>
      <UILanguage>en-US</UILanguage>
      <UserLocale>en-US</UserLocale>
    </component>
    <component name="Microsoft-Windows-Setup" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
      <UserData>
        ` + keyXML + `
        <AcceptEula>true</AcceptEula>
        <FullName>` + userEsc + `</FullName>
        <Organization>` + org + `</Organization>
      </UserData>
    </component>
  </settings>
  <settings pass="specialize">
    <component name="Microsoft-Windows-Shell-Setup" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
      <ComputerName>` + hostEsc + `</ComputerName>
      <TimeZone>` + xmlEscape(tz) + `</TimeZone>
    </component>` + rdp + tcp + dns + `
  </settings>
  <settings pass="oobeSystem">
    <component name="Microsoft-Windows-Shell-Setup" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
      <UserAccounts>
        <AdministratorPassword>
          <Value>` + pass + `</Value>
          <PlainText>true</PlainText>
        </AdministratorPassword>` + localAccount + `
      </UserAccounts>
      <OOBE>
        <HideEULAPage>true</HideEULAPage>
        <HideLocalAccountScreen>true</HideLocalAccountScreen>
        <HideOEMRegistrationScreen>true</HideOEMRegistrationScreen>
        <HideOnlineAccountScreens>true</HideOnlineAccountScreens>
        <HideWirelessSetupInOOBE>true</HideWirelessSetupInOOBE>
        <NetworkLocation>Work</NetworkLocation>
        <ProtectYourPC>3</ProtectYourPC>
      </OOBE>
      <AutoLogon>
        <Enabled>true</Enabled>
        <Username>` + xmlEscape(autoUser) + `</Username>
        <Password>
          <Value>` + pass + `</Value>
          <PlainText>true</PlainText>
        </Password>
        <LogonCount>1</LogonCount>
      </AutoLogon>
      <FirstLogonCommands>
        <SynchronousCommand wcm:action="add">
          <Order>1</Order>
          <CommandLine>cmd.exe /c netsh advfirewall firewall set rule group="@FirewallAPI.dll,-28752" new enable=Yes</CommandLine>
        </SynchronousCommand>
        <SynchronousCommand wcm:action="add">
          <Order>2</Order>
          <CommandLine>cmd.exe /c reg add "HKLM\SYSTEM\CurrentControlSet\Control\Terminal Server" /v fDenyTSConnections /t REG_DWORD /d 0 /f</CommandLine>
        </SynchronousCommand>
      </FirstLogonCommands>
    </component>
  </settings>
</unattend>
`
}

func windowsNetworkXML(netcfg model.NetConfig, pxeMAC string) (tcp, dns string) {
	var ifaces []string
	var dnsIfaces []string
	n := 0
	for _, nic := range netcfg.NICs {
		if nic.Type() != model.NICEthernet {
			continue
		}
		if nic.Method != "static" || strings.TrimSpace(nic.Address) == "" {
			continue
		}
		n++
		ip, prefix := splitHostCIDR(nic.Address)
		if ip == "" {
			continue
		}
		id := windowsMACIdent(nic.MAC, pxeMAC, n)
		gw := strings.TrimSpace(nic.Gateway)
		route := ""
		if gw != "" {
			route = fmt.Sprintf(`
        <Routes>
          <Route wcm:action="add" wcm:keyValue="1">
            <Identifier>0</Identifier>
            <Prefix>0.0.0.0/0</Prefix>
            <NextHopAddress>%s</NextHopAddress>
          </Route>
        </Routes>`, xmlEscape(gw))
		}
		ifaces = append(ifaces, fmt.Sprintf(`
      <Interface wcm:action="add">
        <Identifier>%s</Identifier>
        <Ipv4Settings>
          <DhcpEnabled>false</DhcpEnabled>
        </Ipv4Settings>
        <UnicastIpAddresses>
          <IpAddress wcm:action="add" wcm:keyValue="1">%s/%s</IpAddress>
        </UnicastIpAddresses>%s
      </Interface>`, xmlEscape(id), xmlEscape(ip), xmlEscape(prefix), route))
		if len(nic.DNS) > 0 {
			var ips []string
			for i, d := range nic.DNS {
				d = strings.TrimSpace(d)
				if d == "" {
					continue
				}
				ips = append(ips, fmt.Sprintf(`          <IpAddress wcm:action="add" wcm:keyValue="%d">%s</IpAddress>`, i+1, xmlEscape(d)))
			}
			if len(ips) > 0 {
				dnsIfaces = append(dnsIfaces, fmt.Sprintf(`
      <Interface wcm:action="add">
        <Identifier>%s</Identifier>
        <DNSServerSearchOrder>
%s
        </DNSServerSearchOrder>
      </Interface>`, xmlEscape(id), strings.Join(ips, "\n")))
			}
		}
	}
	if len(ifaces) == 0 {
		return "", ""
	}
	tcp = `
    <component name="Microsoft-Windows-TCPIP" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
      <Interfaces>` + strings.Join(ifaces, "") + `
      </Interfaces>
    </component>`
	if len(dnsIfaces) > 0 {
		dns = `
    <component name="Microsoft-Windows-DNS-Client" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
      <Interfaces>` + strings.Join(dnsIfaces, "") + `
      </Interfaces>
    </component>`
	}
	return tcp, dns
}

func windowsMACIdent(mac, fallback string, n int) string {
	mac = strings.TrimSpace(mac)
	if mac == "" {
		mac = fallback
	}
	mac = strings.ToUpper(strings.NewReplacer(":", "-", ".", "-").Replace(mac))
	if mac == "" {
		return fmt.Sprintf("Local Area Connection %d", n)
	}
	return mac
}

func splitHostCIDR(addr string) (ip, prefix string) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "", "24"
	}
	if ipn, ipnet, err := net.ParseCIDR(addr); err == nil {
		ones, _ := ipnet.Mask.Size()
		return ipn.String(), strconv.Itoa(ones)
	}
	if net.ParseIP(addr) != nil {
		return addr, "24"
	}
	host, p, ok := strings.Cut(addr, "/")
	if ok {
		return host, p
	}
	return addr, "24"
}

func xmlEscape(s string) string {
	return html.EscapeString(s)
}
