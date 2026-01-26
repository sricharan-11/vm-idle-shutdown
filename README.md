# IdleShutdown Agent

A lightweight service for RHEL VMs that automatically shuts down idle virtual machines to save costs.

The agent monitors CPU usage and logged-in users. When both conditions are met for the configured duration, the VM is safely shut down.

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

The agent checks two conditions continuously:

| Condition | Default |
|-----------|---------|
| CPU usage below threshold | < 25% for 60 minutes |
| No users logged in | 0 users for 60 minutes |

**When both conditions are true, the VM shuts down automatically.**

---

## Configuration

The configuration file is located at `/etc/idleshutdown/config.ini`:

```ini
[monitoring]
# Duration in minutes to monitor CPU usage
cpu_check_minutes = 60

# Duration in minutes to check for logged-in users  
user_check_minutes = 60

# CPU usage threshold percentage
cpu_threshold = 25
```

### Editing Configuration

```bash
# Edit the config file
sudo vi /etc/idleshutdown/config.ini

# Restart the service to apply changes
sudo systemctl restart IdleShutdown
```

---

## Service Management

```bash
# Check if the service is running
sudo systemctl status IdleShutdown

# View live logs
sudo journalctl -u IdleShutdown -f

# Stop the service (prevents auto-shutdown)
sudo systemctl stop IdleShutdown

# Start the service
sudo systemctl start IdleShutdown

# Disable auto-start on boot
sudo systemctl disable IdleShutdown

# Re-enable auto-start on boot
sudo systemctl enable IdleShutdown
```

---

## Requirements

- RHEL 7, 8, or 9 (also works on CentOS, Rocky Linux, AlmaLinux)
- Root access for installation
- `curl` or `wget` for one-line install

---

## Installed Files

| File | Purpose |
|------|---------|
| `/usr/local/bin/idleshutdown` | The agent binary |
| `/etc/idleshutdown/config.ini` | Configuration file |
| `/etc/systemd/system/IdleShutdown.service` | Systemd service |

---

## Troubleshooting

### Service won't start
```bash
# Check logs for errors
sudo journalctl -u IdleShutdown -e
```

### Test without actual shutdown
```bash
# Run in dry-run mode (logs what would happen, but doesn't shut down)
sudo /usr/local/bin/idleshutdown --dry-run
```

### VM shut down unexpectedly
Check the logs to see why:
```bash
sudo journalctl -u IdleShutdown | grep -i shutdown
```

---

## License

MIT License
