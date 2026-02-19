#!/bin/bash
#
# IdleShutdown Agent Installation Script for RHEL VMs
# 
# USAGE:
#   sudo ./install.sh
#
# WHAT THIS SCRIPT DOES:
#   1. Copies the 'idleshutdown' binary to /usr/local/bin/
#   2. Creates /etc/idleshutdown/ directory
#   3. Copies config.ini to /etc/idleshutdown/config.ini (preserves existing)
#   3b. Copies default.ini to /etc/idleshutdown/default.ini
#   4. Installs the systemd service unit file
#   5. Enables and starts the IdleShutdown service
#

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Installation paths
BINARY_NAME="idleshutdown"
BINARY_DEST="/usr/local/bin/${BINARY_NAME}"
CONFIG_DIR="/etc/idleshutdown"
CONFIG_FILE="${CONFIG_DIR}/config.ini"
DEFAULTS_FILE="${CONFIG_DIR}/default.ini"
SERVICE_NAME="IdleShutdown"
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"

# Script directory (where install.sh is located)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "${SCRIPT_DIR}")"

echo -e "${CYAN}============================================${NC}"
echo -e "${CYAN}  IdleShutdown Agent Installer${NC}"
echo -e "${CYAN}============================================${NC}"
echo ""

# Check if running as root
if [[ $EUID -ne 0 ]]; then
   echo -e "${RED}ERROR: This script must be run as root (use sudo)${NC}"
   exit 1
fi

# Check if binary exists in project directory
BINARY_SOURCE="${PROJECT_DIR}/${BINARY_NAME}"
if [[ ! -f "${BINARY_SOURCE}" ]]; then
    echo -e "${RED}ERROR: Binary not found at ${BINARY_SOURCE}${NC}"
    echo -e "${YELLOW}Please ensure the 'idleshutdown' binary is in the project root.${NC}"
    echo -e "${YELLOW}Build it with: GOOS=linux GOARCH=amd64 go build -o idleshutdown ./cmd/idleshutdown/${NC}"
    exit 1
fi

echo -e "${GREEN}✓ Found binary: ${BINARY_SOURCE}${NC}"

# Stop existing service if running
if systemctl is-active --quiet ${SERVICE_NAME} 2>/dev/null; then
    echo -e "${YELLOW}→ Stopping existing ${SERVICE_NAME} service...${NC}"
    systemctl stop ${SERVICE_NAME}
fi

# Step 1: Install binary
echo -e "${CYAN}[1/6]${NC} Installing binary to ${BINARY_DEST}..."
cp "${BINARY_SOURCE}" "${BINARY_DEST}"
chmod 755 "${BINARY_DEST}"
echo -e "${GREEN}      ✓ Binary installed${NC}"

# Step 2: Create config directory
echo -e "${CYAN}[2/6]${NC} Creating config directory ${CONFIG_DIR}..."
mkdir -p "${CONFIG_DIR}"
echo -e "${GREEN}      ✓ Directory created${NC}"

# Step 3: Install config file
echo -e "${CYAN}[3/6]${NC} Installing configuration file..."
if [[ -f "${CONFIG_FILE}" ]]; then
    echo -e "${YELLOW}      ⚠ Config file exists - keeping existing configuration${NC}"
    echo -e "${YELLOW}        To reset: rm ${CONFIG_FILE} && re-run install.sh${NC}"
else
    cp "${PROJECT_DIR}/config/config.ini" "${CONFIG_FILE}"
    chmod 644 "${CONFIG_FILE}"
    echo -e "${GREEN}      ✓ Configuration installed to ${CONFIG_FILE}${NC}"
fi

# Step 4: Install calibration defaults
echo -e "${CYAN}[4/6]${NC} Installing calibration defaults..."
if [[ -f "${DEFAULTS_FILE}" ]]; then
    echo -e "${YELLOW}      ⚠ Defaults file exists - keeping existing values${NC}"
else
    cp "${PROJECT_DIR}/config/default.ini" "${DEFAULTS_FILE}"
    chmod 644 "${DEFAULTS_FILE}"
    echo -e "${GREEN}      ✓ Defaults installed to ${DEFAULTS_FILE}${NC}"
fi

# Step 5: Install systemd service
echo -e "${CYAN}[5/6]${NC} Installing systemd service..."
cp "${SCRIPT_DIR}/idleshutdown.service" "${SERVICE_FILE}"
chmod 644 "${SERVICE_FILE}"
systemctl daemon-reload
echo -e "${GREEN}      ✓ Service installed${NC}"

# Step 6: Enable and start service
echo -e "${CYAN}[6/6]${NC} Enabling and starting service..."
systemctl enable ${SERVICE_NAME} --quiet
systemctl start ${SERVICE_NAME}
echo -e "${GREEN}      ✓ Service started${NC}"

# Verify
sleep 2
echo ""
if systemctl is-active --quiet ${SERVICE_NAME}; then
    echo -e "${GREEN}============================================${NC}"
    echo -e "${GREEN}  ✓ Installation Complete!${NC}"
    echo -e "${GREEN}============================================${NC}"
    echo ""
    echo -e "${CYAN}Service Status:${NC}"
    systemctl status ${SERVICE_NAME} --no-pager -l | head -10
    echo ""
    echo -e "${CYAN}Configuration:${NC}  ${CONFIG_FILE}"
    echo -e "${CYAN}Binary:${NC}         ${BINARY_DEST}"
    echo -e "${CYAN}Service:${NC}        ${SERVICE_FILE}"
    echo ""
    echo -e "${CYAN}Useful Commands:${NC}"
    echo "  View logs:     journalctl -u ${SERVICE_NAME} -f"
    echo "  Restart:       sudo systemctl restart ${SERVICE_NAME}"
    echo "  Stop:          sudo systemctl stop ${SERVICE_NAME}"
    echo "  Edit config:   sudo vi ${CONFIG_FILE}"
else
    echo -e "${RED}============================================${NC}"
    echo -e "${RED}  ✗ Service failed to start${NC}"
    echo -e "${RED}============================================${NC}"
    echo ""
    echo "Check logs with: journalctl -u ${SERVICE_NAME} -e"
    exit 1
fi
