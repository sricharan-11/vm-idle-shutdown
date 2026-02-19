# IdleShutdown Agent

A lightweight systemd service for RHEL VMs that monitors CPU usage and logged-in users, automatically shutting down VMs that are idle.

## Quick Install

```bash
curl -sSL https://raw.githubusercontent.com/sricharan-11/vm-idle-shutdown/main/scripts/online-install.sh | sudo bash
```

## Quick Uninstall

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

The mode is determined by the **presence or absence** of `cpu_threshold` in `config.ini`:

#### Auto Mode (default — `cpu_threshold` commented out)

Out of the box, `cpu_threshold` is commented out. The agent self-calibrates:

1. **Learning phase** — The agent collects CPU data for 24h. Shutdown evaluation is **paused** during this time.
2. **Initial calibration** — After 24h, the agent analyzes CPU patterns, finds the idle baseline, and sets `threshold = baseline + 3%` (minimum 5%).
3. **Weekly recalibration** — Every 7 days, the agent re-analyzes 72h of data and adjusts.

The config file shows a live status banner:

```ini
# ┌──────────────────────────────────────────────────────────┐
# │  ⚡ AUTO-MANAGED — to set manually, uncomment below      │
# │  Last calibrated : 2026-02-19 15:30 UTC                 │
# │  Idle baseline   : 2.4%                                 │
# │  Current value   : 5% (active)                          │
# │  Next calibration: ~2026-02-26                          │
# └──────────────────────────────────────────────────────────┘
# cpu_threshold = 25
```

#### Manual Mode (uncomment `cpu_threshold`)

Simply uncomment `cpu_threshold` and set your value. The agent uses it as-is — no calibration runs.

```ini
cpu_threshold = 30
```

To switch back to auto: comment out the line again and restart the service.

---

## Configuration

### `/etc/idleshutdown/config.ini`

```ini
[monitoring]
cpu_check_minutes = 60       # How long CPU must be idle before shutdown
user_check_minutes = 60      # How long zero users before shutdown
# cpu_threshold = 25         # Commented = Auto | Uncommented = Manual
```

### `/etc/idleshutdown/default.ini`

Calibration timing parameters (only used in auto mode):

```ini
[calibration]
initial_tracking_hours = 24        # Hours before first calibration
recalibration_interval_days = 7    # Days between recalibrations
recalibration_tracking_hours = 72  # Hours of data to analyze
```

---

## Installed Files

| File | Purpose |
|------|---------|
| `/usr/local/bin/idleshutdown` | Agent binary |
| `/etc/idleshutdown/config.ini` | Main configuration |
| `/etc/idleshutdown/default.ini` | Calibration timing defaults |
| `/etc/idleshutdown/calibration.state` | Auto-calibration state (auto mode) |
| `/etc/systemd/system/IdleShutdown.service` | Systemd service unit |

## Useful Commands

```bash
# View live logs
journalctl -u IdleShutdown -f

# Restart service
sudo systemctl restart IdleShutdown

# Check status
sudo systemctl status IdleShutdown

# Edit main config
sudo vi /etc/idleshutdown/config.ini

# Edit calibration timings
sudo vi /etc/idleshutdown/default.ini
```

## Building from Source

```bash
GOOS=linux GOARCH=amd64 go build -o idleshutdown ./cmd/idleshutdown/
sudo ./scripts/install.sh
```

## Troubleshooting

| Symptom | Check |
|---------|-------|
| Agent not shutting down VM | `journalctl -u IdleShutdown -e` — check if learning phase is active |
| "Learning phase" in logs | Normal for first 24h in auto mode |
| Threshold too aggressive | Switch to manual: uncomment `cpu_threshold` in config.ini |
| Calibration timings | Edit `/etc/idleshutdown/default.ini` and restart |
