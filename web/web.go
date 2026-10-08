package web

import "embed"

// AdminHTML contains the embedded HTML content for the simplysocket Admin Control Center dashboard.
//
//go:embed templates/admin/* templates/admin/sections/*
var AdminTemplatesFS embed.FS

// AdminAPIsJSON contains the embedded JSON catalog of router APIs generated from comments and routes.
//
//go:embed admin_apis.json
var AdminAPIsJSON []byte

// AdminJS contains the embedded JavaScript for the admin dashboard.
//
//go:embed admin.js
var AdminJS []byte
