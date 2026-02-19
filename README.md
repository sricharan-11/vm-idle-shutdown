# IdleShutdown Agent

A lightweight service for RHEL VMs that automatically shuts down idle virtual machines to save costs.

The agent monitors CPU usage and logged-in users. When the VM has been idle long enough, it safely shuts down.

## Quick Install

```bash
curl -sSL https://raw.githubusercontent.com/sricharan-11/vm-idle-shutdown/main/scripts/online-install.sh | sudo bash
```

Or with wget:
```bash
wget -qO- https://raw.githubusercontent.com/sricharan-11/vm-idle-shutdown/main/scripts/online-install.sh | sudo bash
```

## Uninstall

```bash
curl -sSL https://raw.githubusercontent.com/sricharan-11/vm-idle-shutdown/main/scripts/online-uninstall.sh | sudo bash
```

---

## How It Works

The agent shuts down the VM when **both** conditions are true continuously:

| Condition | Default |
|-----------|---------|
| CPU usage below threshold | for 60 minutes |
| No users logged in | for 60 minutes |

### CPU Threshold: Auto vs Manual

The agent supports two modes for determining the CPU threshold, set via `cpu_mode` in the config file.

#### `cpu_mode = auto` (default)

The agent **self-calibrates** the idle CPU threshold — no manual tuning needed.

**How calibration works:**
1. After **24 hours** of data collection, the agent analyzes CPU usage patterns
2. It scans the history using sliding 30-minute windows to find periods of stable, low CPU (standard deviation < 1%, fallback: 2%)
3. It takes the **minimum average CPU** across all qualifying idle windows
4. Sets `cpu_threshold = idle_avg + 3%` and restarts
5. Every **7 days**, it recalibrates using the last **72 hours** of data for a more accurate reading

The `cpu_threshold` in the config file is **automatically updated** after each calibration.

#### `cpu_mode = manual`

Uses the `cpu_threshold` value in the config file as-is. No calibration is performed.

---

## Configuration

The configuration file is at `/etc/idleshutdown/config.ini`:

```ini
[monitoring]
# Minutes of sustained low CPU before shutdown (x)
cpu_check_minutes = 60

# Minutes of no logged-in users before shutdown (y)
user_check_minutes = 60

# CPU threshold % — auto-updated in auto mode, fixed in manual mode (z)
cpu_threshold = 25

# auto = self-calibrating | manual = use cpu_threshold above
cpu_mode = auto
```

### Applying Config Changes

```bash
sudo vi /etc/idleshutdown/config.ini
sudo systemctl restart IdleShutdown
```

---

## Service Management

```bash
# Check service status
sudo systemctl status IdleShutdown

# View live logs (including calibration events)
sudo journalctl -u IdleShutdown -f

# Stop the service (prevents auto-shutdown)
sudo systemctl stop IdleShutdown

# Start / Restart
sudo systemctl start IdleShutdown
sudo systemctl restart IdleShutdown

# Disable / Enable auto-start on boot
sudo systemctl disable IdleShutdown
sudo systemctl enable IdleShutdown
```

---

## Requirements

- RHEL 7, 8, or 9 (also works on CentOS, Rocky Linux, AlmaLinux)
- Root access
- `curl` or `wget`

---

## Installed Files

| Path | Purpose |
|------|---------|
| `/usr/local/bin/idleshutdown` | Agent binary |
| `/etc/idleshutdown/config.ini` | Configuration |
| `/etc/idleshutdown/calibration.state` | Auto-calibration state (auto mode) |
| `/etc/systemd/system/IdleShutdown.service` | Systemd service |

---

## Troubleshooting

**Service won't start:**
```bash
sudo journalctl -u IdleShutdown -e
```

**Test without actual shutdown:**
```bash
sudo /usr/local/bin/idleshutdown --dry-run
```

**VM shut down unexpectedly:**
```bash
sudo journalctl -u IdleShutdown | grep -i shutdown
```

**Force recalibration (auto mode):**
```bash
sudo rm /etc/idleshutdown/calibration.state
sudo systemctl restart IdleShutdown
```

---

## License

MIT License
