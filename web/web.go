package web

import _ "embed"

// AdminHTML contains the embedded HTML content for the simplysocket Admin Control Center dashboard.
//
//go:embed admin.html
var AdminHTML []byte
