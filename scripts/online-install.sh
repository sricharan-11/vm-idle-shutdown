#!/bin/bash
#
# IdleShutdown Agent - Online Installer
# 
# ONE-LINE INSTALL:
#   curl -sSL https://raw.githubusercontent.com/sricharan-11/vm-idle-shutdown/main/scripts/online-install.sh | sudo bash
#

set -e

# ============================================
# CONFIGURATION
# ============================================
GITHUB_USER="sricharan-11"
GITHUB_REPO="vm-idle-shutdown"
GITHUB_BRANCH="main"

# GitHub raw content base URL
BASE_URL="https://raw.githubusercontent.com/${GITHUB_USER}/${GITHUB_REPO}/${GITHUB_BRANCH}"

# ============================================
# Installation paths
# ============================================
BINARY_NAME="idleshutdown"
BINARY_DEST="/usr/local/bin/${BINARY_NAME}"
CONFIG_DIR="/etc/idleshutdown"
CONFIG_FILE="${CONFIG_DIR}/config.ini"
DEFAULTS_FILE="${CONFIG_DIR}/default.ini"
SERVICE_NAME="IdleShutdown"
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

echo -e "${CYAN}============================================${NC}"
echo -e "${CYAN}  IdleShutdown Agent - Online Installer${NC}"
echo -e "${CYAN}============================================${NC}"
echo ""

# Check if running as root
if [[ $EUID -ne 0 ]]; then
   echo -e "${RED}ERROR: This script must be run as root${NC}"
   echo "Usage: curl -sSL <URL> | sudo bash"
   exit 1
fi

# Check for curl or wget
if command -v curl &> /dev/null; then
    DOWNLOADER="curl -sSL -o"
elif command -v wget &> /dev/null; then
    DOWNLOADER="wget -q -O"
else
    echo -e "${RED}ERROR: curl or wget is required${NC}"
    exit 1
fi

# Create temp directory
TEMP_DIR=$(mktemp -d)
trap "rm -rf ${TEMP_DIR}" EXIT

# Stop existing service if running
if systemctl is-active --quiet ${SERVICE_NAME} 2>/dev/null; then
    echo -e "${YELLOW}→ Stopping existing ${SERVICE_NAME} service...${NC}"
    systemctl stop ${SERVICE_NAME}
fi

# Step 1: Download binary from main branch
echo -e "${CYAN}[1/6]${NC} Downloading binary..."
${DOWNLOADER} "${TEMP_DIR}/${BINARY_NAME}" "${BASE_URL}/${BINARY_NAME}"
if [[ ! -f "${TEMP_DIR}/${BINARY_NAME}" ]] || [[ ! -s "${TEMP_DIR}/${BINARY_NAME}" ]]; then
    echo -e "${RED}ERROR: Failed to download binary${NC}"
    exit 1
fi
# Verify it's actually a binary (ELF file)
if ! file "${TEMP_DIR}/${BINARY_NAME}" | grep -q "ELF"; then
    echo -e "${RED}ERROR: Downloaded file is not a valid Linux binary${NC}"
    echo "Downloaded content:"
    head -c 200 "${TEMP_DIR}/${BINARY_NAME}"
    exit 1
fi
cp "${TEMP_DIR}/${BINARY_NAME}" "${BINARY_DEST}"
chmod 755 "${BINARY_DEST}"
echo -e "${GREEN}      ✓ Binary installed to ${BINARY_DEST}${NC}"

# Step 2: Create config directory
echo -e "${CYAN}[2/6]${NC} Creating config directory..."
mkdir -p "${CONFIG_DIR}"
echo -e "${GREEN}      ✓ Directory created${NC}"

# Step 3: Download config file
echo -e "${CYAN}[3/6]${NC} Installing configuration..."
if [[ -f "${CONFIG_FILE}" ]]; then
    echo -e "${YELLOW}      ⚠ Config exists - keeping current configuration${NC}"
else
    ${DOWNLOADER} "${CONFIG_FILE}" "${BASE_URL}/config/config.ini"
    chmod 644 "${CONFIG_FILE}"
    echo -e "${GREEN}      ✓ Configuration installed${NC}"
fi

# Step 4: Download calibration defaults
echo -e "${CYAN}[4/6]${NC} Installing calibration defaults..."
if [[ -f "${DEFAULTS_FILE}" ]]; then
    echo -e "${YELLOW}      ⚠ Defaults file exists - keeping existing values${NC}"
else
    ${DOWNLOADER} "${DEFAULTS_FILE}" "${BASE_URL}/config/default.ini"
    chmod 644 "${DEFAULTS_FILE}"
    echo -e "${GREEN}      ✓ Defaults installed${NC}"
fi

# Step 5: Download and install systemd service
echo -e "${CYAN}[5/6]${NC} Installing systemd service..."
${DOWNLOADER} "${SERVICE_FILE}" "${BASE_URL}/scripts/idleshutdown.service"
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
    echo -e "${CYAN}Configuration:${NC}  ${CONFIG_FILE}"
    echo -e "${CYAN}Service:${NC}        systemctl status ${SERVICE_NAME}"
    echo -e "${CYAN}Logs:${NC}           journalctl -u ${SERVICE_NAME} -f"
    echo ""
    systemctl status ${SERVICE_NAME} --no-pager -l | head -8
else
    echo -e "${RED}ERROR: Service failed to start${NC}"
    echo "Check logs: journalctl -u ${SERVICE_NAME} -e"
    exit 1
fi
