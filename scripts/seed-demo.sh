#!/usr/bin/env bash
set -euo pipefail

# FactDB demo data seeder.
# Start the FactDB server before running this script.
#
# Usage:
#   chmod +x scripts/seed-demo.sh
#   ./scripts/seed-demo.sh
#
# Optional server URL:
#   SERVER=http://127.0.0.1:8080 ./scripts/seed-demo.sh

SERVER="${SERVER:-http://127.0.0.1:8080}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

FG=(./bin/factdb-client -server "$SERVER")

echo "Checking server: $SERVER"
"${FG[@]}" health >/dev/null
echo "Server is available."
echo

echo "Creating entities..."

# Virtual machines
"${FG[@]}" create --type VirtualMachine --id demo-vm-web-01
"${FG[@]}" create --type VirtualMachine --id demo-vm-db-01
"${FG[@]}" create --type VirtualMachine --id demo-vm-worker-01

# Teams
"${FG[@]}" create --type Team --id demo-team-platform
"${FG[@]}" create --type Team --id demo-team-data

# Network and application
"${FG[@]}" create --type Network --id demo-network-app
"${FG[@]}" create --type Application --id demo-app-shop

echo
echo "Adding facts from different sources..."

# Web VM: multiple IPs and facts from different systems
"${FG[@]}" fact demo-vm-web-01 ipAddresses \
  '["192.168.2.15","10.0.0.15"]' \
  --source netbox --verified

"${FG[@]}" fact demo-vm-web-01 hostname \
  '"web-01.example.internal"' \
  --source dns-sync --verified

"${FG[@]}" fact demo-vm-web-01 powerState \
  '"running"' \
  --source hypervisor-api --verified

"${FG[@]}" fact demo-vm-web-01 environment \
  '"production"' \
  --source deployment-pipeline --verified

# Database VM
"${FG[@]}" fact demo-vm-db-01 ipAddress \
  '"192.168.2.20"' \
  --source hypervisor-api --verified

"${FG[@]}" fact demo-vm-db-01 hostname \
  '"db-01.example.internal"' \
  --source dns-sync --verified

"${FG[@]}" fact demo-vm-db-01 powerState \
  '"running"' \
  --source hypervisor-api --verified

# Intentionally unverified information
"${FG[@]}" fact demo-vm-db-01 backupStatus \
  '"unknown"' \
  --source manual-note

# Worker VM: IP fact is deliberately unverified
"${FG[@]}" fact demo-vm-worker-01 ipAddresses \
  '["192.168.3.31","10.0.0.31"]' \
  --source discovery-scan

"${FG[@]}" fact demo-vm-worker-01 hostname \
  '"worker-01.example.internal"' \
  --source dns-sync --verified

"${FG[@]}" fact demo-vm-worker-01 powerState \
  '"stopped"' \
  --source hypervisor-api --verified

# Team facts
"${FG[@]}" fact demo-team-platform displayName \
  '"Platform Engineering"' \
  --source corporate-directory --verified

"${FG[@]}" fact demo-team-data displayName \
  '"Data Engineering"' \
  --source corporate-directory --verified

# Network facts
"${FG[@]}" fact demo-network-app cidr \
  '"192.168.2.0/24"' \
  --source network-design --verified

# Application facts
"${FG[@]}" fact demo-app-shop displayName \
  '"Shop Application"' \
  --source service-catalog --verified

"${FG[@]}" fact demo-app-shop criticality \
  '"high"' \
  --source service-catalog --verified

echo
echo "Creating relationships from different sources..."

# Ownership relationships from the CMDB
"${FG[@]}" relate demo-vm-web-01 ownedBy \
  demo-team-platform --source cmdb

"${FG[@]}" relate demo-vm-db-01 ownedBy \
  demo-team-data --source cmdb

"${FG[@]}" relate demo-vm-worker-01 ownedBy \
  demo-team-platform --source cmdb

# Network relationships from network inventory
"${FG[@]}" relate demo-vm-web-01 connectedTo \
  demo-network-app --source network-inventory

"${FG[@]}" relate demo-vm-db-01 connectedTo \
  demo-network-app --source network-inventory

# Application relationships from different sources
"${FG[@]}" relate demo-app-shop runsOn \
  demo-vm-web-01 --source deployment-manifest

"${FG[@]}" relate demo-app-shop dependsOn \
  demo-vm-db-01 --source architecture-catalog

echo
echo "Demo data created successfully."
echo
echo "Try these commands:"
echo
echo "# Find VMs in the subnet"
echo "./bin/factdb-client -server '$SERVER' query-ip 192.168.2.0/24 --type VirtualMachine"
echo
echo "# Require verified IP facts"
echo "./bin/factdb-client -server '$SERVER' query-ip 192.168.2.0/24 --type VirtualMachine --verified"
echo
echo "# Find VMs owned by the platform team"
echo "./bin/factdb-client -server '$SERVER' relationships demo-team-platform --direction incoming --type ownedBy"
echo
echo "# Inspect outgoing relationships from the web VM"
echo "./bin/factdb-client -server '$SERVER' relationships demo-vm-web-01 --direction outgoing"
echo
echo "# Inspect all facts and relationships for the web VM"
echo "./bin/factdb-client -server '$SERVER' get demo-vm-web-01"
