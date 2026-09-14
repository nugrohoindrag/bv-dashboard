package app

// DefaultExtensions: modul domain yang di-mount ke API. Diisi bertahap (engineering, security, housekeeping,
// tenantservice, notification, overview, search, sync).
func DefaultExtensions(a *App) []Extension {
	return []Extension{}
}
