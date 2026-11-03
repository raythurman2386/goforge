package settings

import (
	"fmt"
	"strconv"

	"github.com/raythurman2386/goforge/internal/audit"
)

func auditRetentionStatus(v *audit.SettingsView) string {
	if v == nil {
		return ""
	}
	switch {
	case v.ReadOnly:
		return fmt.Sprintf("Set by GOFORGE_AUDIT_RETENTION_DAYS (%d). Unset the variable to edit this here.", v.Days)
	case v.EnvIgnored:
		return "Saved here. GOFORGE_AUDIT_RETENTION_DAYS is also set; this saved value is used."
	case v.Days == 0:
		return "Rows are kept until you delete them. Set 30 to 3650 days to delete older rows once a day."
	default:
		return fmt.Sprintf("Rows older than %d days are deleted daily.", v.Days)
	}
}

func auditRetentionValue(v *audit.SettingsView) string {
	if v == nil {
		return "0"
	}
	return strconv.Itoa(v.Days)
}
