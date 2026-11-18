package upstream

// HardwarePreset defines device-specific tuning options for consumer routers and enterprise OOB controllers.
type HardwarePreset struct {
	ID                     string `json:"id"`
	Name                   string `json:"name"`
	Description            string `json:"description"`
	DefaultEndpoint        string `json:"defaultEndpoint"`
	MaxConcurrentSockets   int    `json:"maxConcurrentSockets"`
	SeedHuaweiCookies      bool   `json:"seedHuaweiCookies"`
	RewriteSingleLabelHost bool   `json:"rewriteSingleLabelHost"`
	PreferLegacyTLS        bool   `json:"preferLegacyTls"`
}

var BuiltinPresets = []HardwarePreset{
	{
		ID:                     "auto",
		Name:                   "Auto / Universal",
		Description:            "Balanced defaults for ISP routers, homelab BMCs, switches, and printers",
		DefaultEndpoint:        "",
		MaxConcurrentSockets:   4,
		SeedHuaweiCookies:      true,
		RewriteSingleLabelHost: true,
		PreferLegacyTLS:        false,
	},
	{
		ID:                     "huawei-zte",
		Name:                   "Huawei / ZTE ISP Router",
		Description:            "WE / TE Data / Vodafone / Orange / Etisalat (HG531, HG532n, H108N, H168N)",
		DefaultEndpoint:        "192.168.1.1",
		MaxConcurrentSockets:   4,
		SeedHuaweiCookies:      true,
		RewriteSingleLabelHost: false,
		PreferLegacyTLS:        true,
	},
	{
		ID:                     "idrac",
		Name:                   "Dell iDRAC 6 / 7",
		Description:            "Rewrites internal BMC hostname redirects & limits parallel sockets",
		DefaultEndpoint:        "192.168.0.120",
		MaxConcurrentSockets:   4,
		SeedHuaweiCookies:      false,
		RewriteSingleLabelHost: true,
		PreferLegacyTLS:        true,
	},
	{
		ID:                     "ilo",
		Name:                   "HPE iLO 2 / 3 / 4",
		Description:            "Caps concurrency to 2 sockets to avoid SessionExhaustedError",
		DefaultEndpoint:        "192.168.1.100",
		MaxConcurrentSockets:   2,
		SeedHuaweiCookies:      false,
		RewriteSingleLabelHost: true,
		PreferLegacyTLS:        true,
	},
	{
		ID:                     "apc",
		Name:                   "APC UPS NMC1 / NMC2",
		Description:            "Conservative concurrency for AP9617–AP9635 network management cards",
		DefaultEndpoint:        "192.168.1.50",
		MaxConcurrentSockets:   2,
		SeedHuaweiCookies:      false,
		RewriteSingleLabelHost: true,
		PreferLegacyTLS:        true,
	},
	{
		ID:                     "cisco-hp",
		Name:                   "Cisco / HP Switch & Firewall",
		Description:            "Catalyst 3650, SG300, ASA, ProCurve 2810, ZyXEL GS1900",
		DefaultEndpoint:        "192.168.1.254",
		MaxConcurrentSockets:   4,
		SeedHuaweiCookies:      false,
		RewriteSingleLabelHost: true,
		PreferLegacyTLS:        true,
	},
	{
		ID:                     "printer-nas",
		Name:                   "Printer EWS / Legacy NAS / Cam",
		Description:            "HP LaserJet/Officejet EWS, ReadyNAS, QNAP, Synology, Hikvision/Dahua",
		DefaultEndpoint:        "192.168.1.200",
		MaxConcurrentSockets:   4,
		SeedHuaweiCookies:      false,
		RewriteSingleLabelHost: true,
		PreferLegacyTLS:        false,
	},
}

// FindPreset looks up a built-in hardware preset by ID, returning the "auto" preset if not found.
func FindPreset(id string) HardwarePreset {
	for _, p := range BuiltinPresets {
		if p.ID == id {
			return p
		}
	}
	return BuiltinPresets[0]
}
