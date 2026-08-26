package provision

import (
	"strings"

	"github.com/Songxwn/Rack-auto/internal/model"
)

// Official Microsoft KMS client setup keys (GVLK).
// https://learn.microsoft.com/windows-server/get-started/kms-client-activation-keys
const DefaultKMSHost = "kms.songxwn.com"

type KMSKey struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Edition string `json:"edition"`
	Label   string `json:"label"`
	Key     string `json:"key"`
}

var kmsKeys = []KMSKey{
	{ID: "2019-standard", Version: "2019", Edition: "standard", Label: "Windows Server 2019 Standard", Key: "N69G4-B89J2-4G8F4-WWYCC-J464C"},
	{ID: "2019-datacenter", Version: "2019", Edition: "datacenter", Label: "Windows Server 2019 Datacenter", Key: "WMDGN-G9PQG-XVVXX-R3X43-63DFG"},
	{ID: "2019-essentials", Version: "2019", Edition: "essentials", Label: "Windows Server 2019 Essentials", Key: "WVDHN-86M7X-466P6-VHXV7-YY726"},
	{ID: "2022-standard", Version: "2022", Edition: "standard", Label: "Windows Server 2022 Standard", Key: "VDYBN-27WPP-V4HQT-9VMD4-VMK7H"},
	{ID: "2022-datacenter", Version: "2022", Edition: "datacenter", Label: "Windows Server 2022 Datacenter", Key: "WX4NM-KYWYW-QJJR4-XV3QB-6VM33"},
	{ID: "2022-datacenter-azure", Version: "2022", Edition: "datacenter-azure", Label: "Windows Server 2022 Datacenter: Azure Edition", Key: "NTBV8-9K7Q8-V27C6-M2BTV-KHMXV"},
	{ID: "2025-standard", Version: "2025", Edition: "standard", Label: "Windows Server 2025 Standard", Key: "TVRH6-WHNXV-R9WG3-9XRFY-MY832"},
	{ID: "2025-datacenter", Version: "2025", Edition: "datacenter", Label: "Windows Server 2025 Datacenter", Key: "D764K-2NDRG-47T6Q-P8T8W-YP6DF"},
	{ID: "2025-datacenter-azure", Version: "2025", Edition: "datacenter-azure", Label: "Windows Server 2025 Datacenter: Azure Edition", Key: "XGN3F-F394H-FD2MY-PP6FD-8MCRC"},
}

func ListKMSKeys() []KMSKey {
	out := make([]KMSKey, len(kmsKeys))
	copy(out, kmsKeys)
	return out
}

func LookupKMSKey(id string) (KMSKey, bool) {
	id = strings.TrimSpace(strings.ToLower(id))
	for _, k := range kmsKeys {
		if k.ID == id {
			return k, true
		}
	}
	return KMSKey{}, false
}

// MatchKMSKeyID picks a GVLK id from image OS version and WIM edition flags/name.
func MatchKMSKeyID(osVersion string, wim model.WIMImage) string {
	ver := strings.TrimSpace(osVersion)
	if ver == "" {
		blob := strings.ToUpper(wim.Name + " " + wim.Description)
		switch {
		case strings.Contains(blob, "2025"):
			ver = "2025"
		case strings.Contains(blob, "2022"):
			ver = "2022"
		case strings.Contains(blob, "2019"):
			ver = "2019"
		}
	}
	ed := classifyWIMEdition(wim)
	id := ver + "-" + ed
	if _, ok := LookupKMSKey(id); ok {
		return id
	}
	if ver != "" {
		fallback := ver + "-standard"
		if _, ok := LookupKMSKey(fallback); ok {
			return fallback
		}
	}
	return "2022-standard"
}

func classifyWIMEdition(wim model.WIMImage) string {
	n := strings.ToUpper(wim.Name + " " + wim.Description + " " + wim.Flags + " " + wim.Edition)
	switch {
	case strings.Contains(n, "AZURE"):
		return "datacenter-azure"
	case strings.Contains(n, "ESSENTIAL"):
		return "essentials"
	case strings.Contains(n, "DATACENTER"):
		return "datacenter"
	default:
		return "standard"
	}
}

// EffectiveProductKey returns the key written into unattend ProductKey.
// Custom product_key wins; otherwise resolve kms_key_id.
func EffectiveProductKey(spec model.InstallSpec) string {
	if key := strings.TrimSpace(spec.ProductKey); key != "" {
		return key
	}
	if k, ok := LookupKMSKey(spec.KMSKeyID); ok {
		return k.Key
	}
	return ""
}

func FindWIMImage(images []model.WIMImage, index int) (model.WIMImage, bool) {
	for _, im := range images {
		if im.Index == index {
			return im, true
		}
	}
	return model.WIMImage{}, false
}
