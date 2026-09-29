#!/usr/bin/env bash
# generate-apis-for-admin
# Scans internal/api/router.go and handler doc comments, generates web/admin_apis.json,
# and synchronizes the dynamic API test bench catalog for the admin panel.

set -e

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$DIR"

echo "Scanning router functions and comments..."
go run scripts/generate_apis_for_admin.go "$DIR"
