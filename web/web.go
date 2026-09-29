package web

import _ "embed"

// AdminHTML contains the embedded HTML content for the simplysocket Admin Control Center dashboard.
//
//go:embed admin.html
var AdminHTML []byte

// AdminAPIsJSON contains the embedded JSON catalog of router APIs generated from comments and routes.
//
//go:embed admin_apis.json
var AdminAPIsJSON []byte
