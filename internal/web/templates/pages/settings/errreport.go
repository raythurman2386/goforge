package settings

import "github.com/raythurman2386/goforge/internal/errreport"

func errReportStatusLine(v *errreport.SettingsView) string {
	switch {
	case v.Unreadable:
		return "Error reporting is off: the saved DSN does not decrypt with the current ENCRYPTION_KEY. Enter it again."
	case v.DSNSet && v.Source == errreport.SourceEnv:
		return "Error reporting is on (from the environment: SENTRY_DSN), environment " + v.EffectiveEnvironment + ". Saving here overrides it."
	case v.DSNSet:
		return "Error reporting is on (from settings), environment " + v.EffectiveEnvironment + "."
	default:
		return "Error reporting is off. Add a Sentry DSN to send GoForge panics and server errors to Sentry."
	}
}

func errReportDSNState(v *errreport.SettingsView) string {
	switch {
	case v.Unreadable:
		return "Saved, but it does not decrypt with the current ENCRYPTION_KEY. Enter it again."
	case v.DSNSet && v.Source == errreport.SourceEnv:
		return "Set from SENTRY_DSN (" + v.DSNHint + "). Type a DSN to store one here instead."
	case v.DSNSet:
		return "Set (" + v.DSNHint + "). Leave blank to keep it; type a new one to replace it."
	default:
		return "Not set. From Sentry → Project settings → Client Keys (DSN). Stored encrypted and never shown again."
	}
}

func errReportDSNPlaceholder(v *errreport.SettingsView) string {
	if v.DSNSet {
		return v.DSNHint + " " + maskedPlaceholder
	}
	return "https://<key>@o0.ingest.sentry.io/<project>"
}
