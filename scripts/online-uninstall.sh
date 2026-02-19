#!/bin/bash
#
# IdleShutdown Agent - Online Uninstaller
#
# ONE-LINE UNINSTALL:
#   curl -sSL https://raw.githubusercontent.com/sricharan-11/vm-idle-shutdown/main/scripts/online-uninstall.sh | sudo bash
#

set -e

# Installation paths
BINARY_PATH="/usr/local/bin/idleshutdown"
CONFIG_DIR="/etc/idleshutdown"
SERVICE_NAME="IdleShutdown"
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

echo -e "${YELLOW}============================================${NC}"
echo -e "${YELLOW}  IdleShutdown Agent - Uninstaller${NC}"
echo -e "${YELLOW}============================================${NC}"
echo ""

if [[ $EUID -ne 0 ]]; then
   echo -e "${RED}ERROR: This script must be run as root${NC}"
   exit 1
fi

# Stop and disable service
echo -e "${CYAN}[1/3]${NC} Stopping service..."
systemctl stop ${SERVICE_NAME} 2>/dev/null || true
systemctl disable ${SERVICE_NAME} 2>/dev/null || true
echo -e "${GREEN}      ✓ Service stopped${NC}"

# Remove files
echo -e "${CYAN}[2/3]${NC} Removing files..."
rm -f "${SERVICE_FILE}"
rm -f "${BINARY_PATH}"
systemctl daemon-reload
echo -e "${GREEN}      ✓ Files removed${NC}"

# Remove config (optional)
echo -e "${CYAN}[3/3]${NC} Removing configuration..."
rm -rf "${CONFIG_DIR}"
echo -e "${GREEN}      ✓ Configuration removed${NC}"

echo ""
echo -e "${GREEN}============================================${NC}"
echo -e "${GREEN}  ✓ Uninstallation Complete!${NC}"
echo -e "${GREEN}============================================${NC}"
