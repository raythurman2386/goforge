# Systemd Deployment

Deploy GoForge as a systemd service on a bare metal server or VM.

## Prerequisites

- Go 1.25+ (for building)
- Docker Engine
- PostgreSQL (external or local)
- Traefik (installed separately)

## Setup

### 1. Build the Binary

```bash
git clone https://github.com/raythurman2386/goforge.git
cd goforge
make build
```

### 2. Create Application Directory

```bash
sudo mkdir -p /opt/goforge
sudo cp bin/goforge /opt/goforge/
sudo cp -r static /opt/goforge/
sudo cp -r templates /opt/goforge/
sudo cp .env.example /opt/goforge/.env
```

### 3. Configure Environment

Edit `/opt/goforge/.env` with your production settings. See [Environment Variables](../configuration/index.md) for all options.

### 4. Install Systemd Service

```bash
sudo cp deploy/goforge.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable goforge
sudo systemctl start goforge
```

### 5. Verify

```bash
sudo systemctl status goforge
sudo journalctl -u goforge -f
```

## Service File

The systemd service file (`deploy/goforge.service`):

```ini
[Unit]
Description=GoForge PaaS
After=network.target postgresql.service docker.service
Requires=postgresql.service docker.service

[Service]
Type=simple
# User=goforge
# Group=docker
# Run as root by default for Docker access, or use a user in the docker group.
User=root
WorkingDirectory=/opt/goforge
ExecStart=/opt/goforge/goforge serve
Restart=always
RestartSec=5
EnvironmentFile=/opt/goforge/.env
# Increase open file limit if necessary
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
```

!!! warning "Runs as root"
    The default service file runs as `User=root`. See the [Running as Non-Root](#running-as-non-root) section below for a more secure setup.

## Managing the Service

```bash
# Start
sudo systemctl start goforge

# Stop
sudo systemctl stop goforge

# Restart
sudo systemctl restart goforge

# Status
sudo systemctl status goforge

# View logs
sudo journalctl -u goforge -f
sudo journalctl -u goforge --since "1 hour ago"
```

## Running Migrations Manually

```bash
cd /opt/goforge
./goforge migrate up
./goforge migrate status
```

## Updating

```bash
# Build new binary
cd /path/to/goforge-source
git pull origin main
make build

# Deploy
sudo systemctl stop goforge
sudo cp bin/goforge /opt/goforge/
sudo cp -r static /opt/goforge/
sudo cp -r templates /opt/goforge/
sudo systemctl start goforge
```

## Running as Non-Root

For improved security, create a dedicated user:

```bash
# Create user
sudo useradd -r -s /bin/false goforge
sudo usermod -aG docker goforge

# Set ownership
sudo chown -R goforge:goforge /opt/goforge

# Update service file
sudo systemctl edit goforge
```

Add:

```ini
[Service]
User=goforge
Group=docker
```

Then reload and restart:

```bash
sudo systemctl daemon-reload
sudo systemctl restart goforge
```
