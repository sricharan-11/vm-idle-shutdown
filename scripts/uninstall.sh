#!/bin/bash
#
# IdleShutdown Agent Uninstallation Script for RHEL VMs
#
# USAGE:
#   sudo ./uninstall.sh
#
# WHAT THIS SCRIPT DOES:
#   1. Stops the IdleShutdown service
#   2. Disables the service from starting on boot
#   3. Removes the systemd service file
#   4. Removes the binary from /usr/local/bin/
#   5. Optionally removes the config directory
#

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Installation paths
BINARY_PATH="/usr/local/bin/idleshutdown"
CONFIG_DIR="/etc/idleshutdown"
SERVICE_NAME="IdleShutdown"
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"

echo -e "${YELLOW}============================================${NC}"
echo -e "${YELLOW}  IdleShutdown Agent Uninstaller${NC}"
echo -e "${YELLOW}============================================${NC}"
echo ""

# Check if running as root
if [[ $EUID -ne 0 ]]; then
   echo -e "${RED}ERROR: This script must be run as root (use sudo)${NC}"
   exit 1
fi

# Step 1: Stop service
echo -e "${CYAN}[1/4]${NC} Stopping service..."
if systemctl is-active --quiet ${SERVICE_NAME} 2>/dev/null; then
    systemctl stop ${SERVICE_NAME}
    echo -e "${GREEN}      ✓ Service stopped${NC}"
else
    echo -e "${YELLOW}      ⚠ Service was not running${NC}"
fi

# Step 2: Disable service
echo -e "${CYAN}[2/4]${NC} Disabling service..."
if systemctl is-enabled --quiet ${SERVICE_NAME} 2>/dev/null; then
    systemctl disable ${SERVICE_NAME} --quiet
    echo -e "${GREEN}      ✓ Service disabled${NC}"
else
    echo -e "${YELLOW}      ⚠ Service was not enabled${NC}"
fi

# Step 3: Remove service file
echo -e "${CYAN}[3/4]${NC} Removing systemd service file..."
if [[ -f "${SERVICE_FILE}" ]]; then
    rm -f "${SERVICE_FILE}"
    systemctl daemon-reload
    echo -e "${GREEN}      ✓ Service file removed${NC}"
else
    echo -e "${YELLOW}      ⚠ Service file not found${NC}"
fi

# Step 4: Remove binary
echo -e "${CYAN}[4/4]${NC} Removing binary..."
if [[ -f "${BINARY_PATH}" ]]; then
    rm -f "${BINARY_PATH}"
    echo -e "${GREEN}      ✓ Binary removed${NC}"
else
    echo -e "${YELLOW}      ⚠ Binary not found${NC}"
fi

# Ask about config removal
echo ""
if [[ -d "${CONFIG_DIR}" ]]; then
    echo -e "${YELLOW}Configuration directory exists: ${CONFIG_DIR}${NC}"
    read -p "Remove configuration directory? (y/N): " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        rm -rf "${CONFIG_DIR}"
        echo -e "${GREEN}✓ Configuration directory removed${NC}"
    else
        echo -e "${CYAN}→ Configuration preserved at ${CONFIG_DIR}${NC}"
    fi
fi

echo ""
echo -e "${GREEN}============================================${NC}"
echo -e "${GREEN}  ✓ Uninstallation Complete!${NC}"
echo -e "${GREEN}============================================${NC}"
