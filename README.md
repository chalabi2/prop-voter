# Prop-Voter

A secure, automated Cosmos governance proposal monitoring and voting bot for Discord.

## Table of Contents

- [Features](#features)
- [Architecture](#architecture)
- [Prerequisites](#prerequisites)
- [Installation](#installation)
- [Configuration](#configuration)
- [Discord Bot Setup](#discord-bot-setup)
- [Binary and Key Management](#binary-and-key-management)
- [Usage](#usage)
- [Health Monitoring](#health-monitoring)
- [Testing](#testing)
- [Docker Deployment](#docker-deployment)
- [Security](#security)
- [Troubleshooting](#troubleshooting)
- [Development](#development)
- [Contributing](#contributing)

## Features

- **Multi-Chain Scanning**: Automatically scans multiple Cosmos chains for new governance proposals
- **Discord Notifications**: Sends real-time notifications when new proposals are detected
- **Secure Voting**: Vote on proposals through Discord commands with secret verification
- **Wallet Security**: Encrypted wallet storage with user authentication
- **Chain Registry Integration**: Automatically discovers chain metadata from the official Cosmos Chain Registry
- **Simplified Configuration**: Just specify chain name, RPC, REST, and wallet key - everything else is auto-discovered
- **Legacy Support**: Maintains compatibility with manual chain configuration
- **Access Control**: Only responds to authorized Discord users
- **Health Monitoring**: Built-in health endpoints for monitoring and alerting
- **Thoroughly Tested**: Comprehensive unit and integration test coverage
- **Auto-Binary Management**: Automatically downloads and updates CLI tools from GitHub releases
- **Secure Key Management**: Encrypted key storage and secure import/export functionality

## Architecture

The system consists of several key components:

1. **Proposal Scanner**: Monitors Cosmos chains via REST APIs
2. **Discord Bot**: Handles notifications and user commands
3. **Voting System**: Executes votes using Cosmos CLI tools
4. **Wallet Manager**: Securely stores and manages wallet keys
5. **Database**: SQLite storage for proposals, votes, and state
6. **Binary Manager**: Automatically downloads and updates CLI tools from GitHub
7. **Key Manager**: Secure key import, storage, and backup functionality

## Prerequisites

- Go 1.21 or later
- Discord bot token and channel setup
- Wallet keys/mnemonics for each chain (CLI tools are auto-managed)

**Note**: CLI tools like `gaiad`, `osmosisd`, etc. are now automatically downloaded and managed. You only need to provide wallet keys.

## Installation

### Method 1: From Source

```bash
# Clone the repository
git clone <repo-url>
cd prop-voter

# Install dependencies
make install-deps

# Build the application
make build
```

### Method 2: Using Go Install

```bash
go install github.com/your-org/prop-voter/cmd/prop-voter@latest
```

### Method 3: Download Binary

Download pre-built binaries from the [releases page](https://github.com/your-org/prop-voter/releases).

### Setup Configuration

```bash
# Create configuration from example
cp config.example.yaml config.yaml

# Create necessary directories
mkdir -p bin keys backups logs

# Set up basic configuration
make setup
```

## Configuration

Prop-Voter supports two configuration formats: the new **Chain Registry format** (recommended) and the **legacy format** for maximum compatibility.

### Chain Registry Configuration (Recommended)

The new simplified format automatically discovers chain metadata from the official [Cosmos Chain Registry](https://github.com/cosmos/chain-registry):

```yaml
# Basic service configuration
database:
  path: "./prop-voter.db"

health:
  enabled: true
  port: 8080
  path: "/health"

# Security settings
security:
  encryption_key: "your-32-char-encryption-key-here"
  vote_secret: "your-secret-phrase-for-voting"

# Binary management
binary_manager:
  enabled: true
  bin_dir: "./bin"
  check_interval: "24h"
  auto_update: false
  backup_old: true

# Key management
key_manager:
  auto_import: false
  key_dir: "./keys"
  backup_keys: true
  encrypt_keys: true

# Discord configuration
discord:
  token: "YOUR_BOT_TOKEN_HERE"
  channel_id: "YOUR_CHANNEL_ID_HERE"
  allowed_user_id: "YOUR_USER_ID_HERE"

# Simplified chain configurations using Chain Registry
chains:
  # Osmosis - just 4 lines needed!
  - chain_name: "osmosis" # Chain Registry identifier
    rpc: "https://rpc-osmosis.blockapsis.com"
    rest: "https://lcd-osmosis.blockapsis.com"
    wallet_key: "my-osmosis-key"
    # Everything else auto-discovered: chain_id, daemon_name, denom, prefix, binary_url, logo, etc.

  # Juno - equally simple
  - chain_name: "juno"
    rpc: "https://rpc-juno.blockapsis.com"
    rest: "https://lcd-juno.blockapsis.com"
    wallet_key: "my-juno-key"

  # You can mix formats - legacy format for unsupported chains
  - name: "Custom Chain"
    chain_id: "custom-1"
    rpc: "https://rpc-custom.example.com"
    rest: "https://lcd-custom.example.com"
    denom: "ucustom"
    prefix: "custom"
    cli_name: "customd"
    wallet_key: "my-custom-key"
    binary_repo:
      enabled: true
      owner: "custom-org"
      repo: "custom-chain"
      asset_pattern: "*linux-amd64*"
```

### Legacy Configuration Format

For chains not in the Chain Registry or when you need full control:

```yaml
chains:
  - name: "Cosmos Hub"
    chain_id: "cosmoshub-4"
    rpc: "https://rpc-cosmoshub.blockapsis.com"
    rest: "https://lcd-cosmoshub.blockapsis.com"
    denom: "uatom"
    prefix: "cosmos"
    cli_name: "gaiad"
    wallet_key: "my-cosmos-key"
    logo_url: "https://example.com/cosmos-logo.png"
    binary_repo:
      enabled: true
      owner: "cosmos"
      repo: "gaia"
      asset_pattern: "*linux-amd64*"
```

### Chain Registry Benefits

Using the Chain Registry format provides:

- **Automatic Updates**: Chain metadata is always current
- **Reduced Configuration**: 70% fewer config lines per chain
- **Consistency**: Uses official chain data from the Cosmos ecosystem
- **Binary Discovery**: Automatically finds the latest compatible binaries
- **Future-Proof**: New chains are supported automatically once added to the registry

### Supported Chain Registry Chains

The following chains are supported through the Chain Registry (just use `chain_name`):

- `cosmoshub` - Cosmos Hub
- `osmosis` - Osmosis DEX
- `juno` - Juno Network
- `akash` - Akash Network
- `kujira` - Kujira
- `stargaze` - Stargaze
- `injective` - Injective Protocol
- `stride` - Stride Zone
- `evmos` - Evmos
- `kava` - Kava
- `secret` - Secret Network
- `terra2` - Terra 2.0
- `persistence` - Persistence
- `sommelier` - Sommelier
- `gravity-bridge` - Gravity Bridge
- `crescent` - Crescent Network
- `chihuahua` - Chihuahua Chain
- `comdex` - Comdex
- `desmos` - Desmos
- `regen` - Regen Network
- `sentinel` - Sentinel
- `cyber` - Cyber
- `iris` - IRISnet
- `fetchai` - Fetch.ai
- `archway` - Archway
- `neutron` - Neutron
- `noble` - Noble
- `composable` - Composable Finance
- `saga` - Saga
- `dymension` - Dymension
- `celestia` - Celestia

And many more! See the complete list: `./prop-voter -registry list`

### Platform-Specific Binary Patterns (Legacy Format)

When using legacy configuration, common asset patterns for different platforms:

- **Linux AMD64**: `*linux-amd64*`
- **Linux ARM64**: `*linux-arm64*`
- **macOS AMD64**: `*darwin-amd64*`
- **macOS ARM64**: `*darwin-arm64*`
- **Windows**: `*windows-amd64*`

## Discord Bot Setup

### Step 1: Create Discord Application

1. Go to the [Discord Developer Portal](https://discord.com/developers/applications)
2. Click "New Application"
3. Give it a name like "Prop-Voter" or "Governance Bot"
4. Click "Create"

### Step 2: Create the Bot

1. In your application, click on "Bot" in the left sidebar
2. Click "Add Bot"
3. Customize your bot:
   - **Username**: Something like "Prop-Voter" or "Gov-Bot"
   - **Avatar**: Upload an icon if you want
4. Under "Privileged Gateway Intents", enable "Message Content Intent" if needed

### Step 3: Get Your Bot Token

1. In the Bot section, under "Token", click "Copy"
2. **Save this token securely** - you'll need it for your config
3. **Never share this token publicly** - treat it like a password

### Step 4: Set Bot Permissions

1. In the Bot section, scroll down to "Bot Permissions"
2. Select these permissions:
   - Send Messages
   - Read Message History
   - Use Slash Commands (optional)
   - Embed Links (for rich proposal displays)

### Step 5: Generate Invite Link

1. Go to "OAuth2" → "URL Generator" in the left sidebar
2. Under "Scopes", select: **bot**
3. Under "Bot Permissions", select the same permissions from Step 4
4. Copy the generated URL at the bottom

### Step 6: Add Bot to Your Server

1. Open the invite URL from Step 5 in your browser
2. Select the Discord server where you want to add the bot
3. Click "Authorize"
4. Complete the CAPTCHA if prompted

### Step 7: Create Dedicated Channel

1. In your Discord server, create a new text channel
2. Name it something like `#governance` or `#prop-voter`
3. Set channel permissions so only you (and trusted admins) can send messages
4. The bot will need "Send Messages" and "Read Message History" permissions

### Step 8: Get Required IDs

#### Get Your User ID

1. Enable Developer Mode in Discord:
   - Settings → Advanced → Developer Mode (toggle ON)
2. Right-click on your username and select "Copy User ID"
3. Save this ID - this ensures only you can control the bot

#### Get Channel ID

1. Right-click on the governance channel you created
2. Select "Copy Channel ID"
3. Save this ID - this is where the bot will send notifications

### Step 9: Test Discord Setup

Before configuring the full bot, test your Discord setup:

```bash
# Quick Discord test
make test-discord TOKEN=your_bot_token CHANNEL=your_channel_id USER=your_user_id
```

This will connect to Discord and verify permissions.

## Binary and Key Management

### Binary Management

The binary manager automatically downloads and updates Cosmos CLI tools from their official GitHub releases.

#### CLI Commands

```bash
# List all managed binaries
./prop-voter -binary list

# Update a specific chain's binary
./prop-voter -binary update "Cosmos Hub"

# Check binary status
./prop-voter -binary check
```

#### Automatic Updates

For production environments:

```yaml
binary_manager:
  auto_update: true
  backup_old: true
  check_interval: "6h"
```

### Key Management

The key manager provides secure import, storage, and management of wallet keys across multiple chains.

#### Key Import Methods

**Manual Import:**

```bash
# Import a key interactively
./prop-voter -key import "Cosmos Hub" "my-validator-key"

# Import from file
./prop-voter -key import "Cosmos Hub" "my-validator-key" /path/to/mnemonic.txt
```

**File-based Import:**

Place mnemonic files in the configured `key_dir`:

```bash
mkdir -p keys
echo "your mnemonic phrase here" > keys/my-validator-key.mnemonic
```

Set `auto_import: true` and the system will automatically import keys on startup.

#### Key Operations

```bash
# List all keys across all chains
./prop-voter -key list

# Export a key (with security warnings)
./prop-voter -key export "Cosmos Hub" "my-validator-key" backup.key

# Create backup of all keys
./prop-voter -key backup ./key-backups

# Validate all required keys exist
./prop-voter -key validate
```

## Usage

### Building and Running

```bash
# Build the application
make build

# Validate configuration and chains
make validate

# Run the application
make run

# Run in debug mode
make run-debug
```

### Discord Commands

Once the bot is running, use these commands in your configured Discord channel:

- `!help` - Show available commands
- `!proposals [chain]` - List recent proposals (optionally filter by chain)
- `!vote <chain> <proposal_id> <vote> <secret>` - Vote on a proposal
- `!status <chain> <proposal_id>` - Show voting status for a proposal

**Vote options**: `yes`, `no`, `abstain`, `no_with_veto`

**Example voting:**

```
!vote cosmoshub-4 123 yes mysecret
```

### Automatic Notifications

The bot will automatically notify you when:

- New proposals are detected on any configured chain
- Proposal voting periods start
- Voting deadlines are approaching

## Health Monitoring

The bot includes built-in health monitoring endpoints for production monitoring and alerting.

### Health Endpoints

- **`GET /health`** - Main health check endpoint

  - Returns overall system health status
  - Includes service status (database, Discord, chains)
  - Provides system metrics (memory, goroutines, scan errors)
  - Returns HTTP 200 for healthy, 206 for degraded, 503 for unhealthy

- **`GET /metrics`** - Prometheus-style metrics

  - Uptime, memory usage, goroutines
  - Scan error counts and chain configuration
  - Compatible with Prometheus/Grafana monitoring

- **`GET /ready`** - Readiness probe
  - Indicates if service is ready to handle requests
  - Useful for Kubernetes readiness probes
  - Returns HTTP 200 when ready, 503 when not ready

### Example Health Response

```json
{
  "status": "healthy",
  "timestamp": "2023-08-13T22:00:00Z",
  "uptime": "2h30m15s",
  "services": {
    "database": "healthy",
    "discord": "configured",
    "chains": "3 configured"
  },
  "metrics": {
    "goroutines": 12,
    "memory_mb": 45,
    "scan_errors": 0,
    "total_chains": 3,
    "active_chains": 3
  },
  "last_scan": "2023-08-13T21:59:30Z"
}
```

### Monitoring Setup

For production monitoring:

1. **Set up health checks** with your monitoring system (Datadog, New Relic, etc.)
2. **Configure alerting** on the health endpoints
3. **Use Prometheus** to scrape the `/metrics` endpoint
4. **Set up Kubernetes** readiness/liveness probes

Example Kubernetes health checks:

```yaml
livenessProbe:
  httpGet:
    path: /health
    port: 8080
  initialDelaySeconds: 30
  periodSeconds: 10

readinessProbe:
  httpGet:
    path: /ready
    port: 8080
  initialDelaySeconds: 5
  periodSeconds: 5
```

## Testing

The project includes comprehensive test coverage:

### Running Tests

```bash
# Run all tests
make test

# Run tests with coverage report
make test-coverage

# Run only unit tests
go test ./config ./internal/...

# Run integration tests
go test ./test/...

# Run specific test
go test -v ./internal/health -run TestHealthHandler

# Benchmark tests
go test -bench=. ./internal/...

# Test Discord setup
make test-discord TOKEN=your_bot_token CHANNEL=your_channel_id USER=your_user_id
```

### Test Coverage

- **Unit Tests**: All major components (config, health, voting, wallet, scanner)
- **Integration Tests**: Full system testing with real database and HTTP servers
- **Benchmark Tests**: Performance testing for critical paths
- **Mock Services**: HTTP test servers for external API testing

### Testing Categories

- Configuration loading and validation
- Health endpoint functionality and monitoring
- Wallet encryption/decryption and storage
- Proposal scanning and processing
- Vote command generation and parsing
- Database operations and relationships

## Docker Deployment

### Basic Docker Setup

Create a `Dockerfile`:

```dockerfile
FROM golang:1.21-alpine AS builder

WORKDIR /app
COPY . .
RUN go mod download
RUN CGO_ENABLED=1 GOOS=linux go build -o prop-voter ./cmd/prop-voter

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/

COPY --from=builder /app/prop-voter .
COPY config.example.yaml config.yaml

EXPOSE 8080
CMD ["./prop-voter", "-config", "config.yaml"]
```

### Docker Compose Setup

Create a `docker-compose.yml`:

```yaml
version: "3.8"

services:
  prop-voter:
    build: .
    ports:
      - "8080:8080"
    volumes:
      - ./config.yaml:/root/config.yaml:ro
      - ./keys:/root/keys:ro
      - ./bin:/root/bin
      - ./data:/root/data
      - ./logs:/root/logs
    environment:
      - LOG_LEVEL=info
    restart: unless-stopped
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8080/health"]
      interval: 30s
      timeout: 10s
      retries: 3

  # Optional: Add Prometheus for monitoring
  prometheus:
    image: prom/prometheus:latest
    ports:
      - "9090:9090"
    volumes:
      - ./prometheus.yml:/etc/prometheus/prometheus.yml
    command:
      - "--config.file=/etc/prometheus/prometheus.yml"
      - "--storage.tsdb.path=/prometheus"
      - "--web.console.libraries=/etc/prometheus/console_libraries"
      - "--web.console.templates=/etc/prometheus/consoles"

  # Optional: Add Grafana for dashboards
  grafana:
    image: grafana/grafana:latest
    ports:
      - "3000:3000"
    environment:
      - GF_SECURITY_ADMIN_PASSWORD=admin
    volumes:
      - grafana-storage:/var/lib/grafana

volumes:
  grafana-storage:
```

### Production Docker Deployment

```bash
# Build and run with docker-compose
docker-compose up -d

# View logs
docker-compose logs -f prop-voter

# Stop services
docker-compose down

# Update and restart
docker-compose pull
docker-compose up -d --force-recreate
```

### SystemD Service Deployment

For Linux systems using systemd, you can run Prop-Voter as a system service directly from your project directory:

1. **Prepare the service file:**

Edit `prop-voter.service` to match your setup:

```bash
# Copy the service file template
cp prop-voter.service prop-voter.service.local

# Edit the service file
nano prop-voter.service.local
```

**Update these lines in the service file:**

```ini
User=YOUR_USERNAME              # Replace with your username (e.g., ubuntu, ec2-user, etc.)
Group=YOUR_GROUP                # Replace with your group (usually same as username)
WorkingDirectory=/path/to/your/prop-voter   # Replace with full path to your prop-voter directory
ExecStart=/path/to/your/prop-voter/prop-voter -config config.yaml   # Full path to your binary
```

**Example for user 'ubuntu' in '/home/ubuntu/prop-voter':**

```ini
User=ubuntu
Group=ubuntu
WorkingDirectory=/home/ubuntu/prop-voter
ExecStart=/home/ubuntu/prop-voter/prop-voter -config config.yaml
```

2. **Install the service file:**

```bash
sudo cp prop-voter.service.local /etc/systemd/system/prop-voter.service
sudo systemctl daemon-reload
```

3. **Enable and start the service:**

```bash
sudo systemctl enable prop-voter
sudo systemctl start prop-voter
```

4. **Check service status:**

```bash
sudo systemctl status prop-voter
sudo journalctl -u prop-voter -f
```

**Benefits of this approach:**

- No need to create system users
- No need to copy files around
- Uses your existing keys and config in place
- Easy to update - just restart the service after rebuilding
- Simpler permissions management

**Directory structure (running in place):**

```
/home/your-user/prop-voter/
├── prop-voter              # Binary
├── config.yaml             # Your configuration
├── keys/                   # Your key files
│   ├── my-cosmos-key.mnemonic
│   └── my-osmosis-key.mnemonic
├── bin/                    # Auto-downloaded CLI tools
├── logs/                   # Application logs
├── prop-voter.db          # SQLite database
└── prop-voter.service     # Service file template
```

### Kubernetes Deployment

Create `k8s-deployment.yaml`:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: prop-voter
spec:
  replicas: 1
  selector:
    matchLabels:
      app: prop-voter
  template:
    metadata:
      labels:
        app: prop-voter
    spec:
      containers:
        - name: prop-voter
          image: your-registry/prop-voter:latest
          ports:
            - containerPort: 8080
          env:
            - name: LOG_LEVEL
              value: "info"
          volumeMounts:
            - name: config
              mountPath: /root/config.yaml
              subPath: config.yaml
            - name: keys
              mountPath: /root/keys
          livenessProbe:
            httpGet:
              path: /health
              port: 8080
            initialDelaySeconds: 30
            periodSeconds: 10
          readinessProbe:
            httpGet:
              path: /ready
              port: 8080
            initialDelaySeconds: 5
            periodSeconds: 5
      volumes:
        - name: config
          configMap:
            name: prop-voter-config
        - name: keys
          secret:
            secretName: prop-voter-keys

---
apiVersion: v1
kind: Service
metadata:
  name: prop-voter-service
spec:
  selector:
    app: prop-voter
  ports:
    - port: 8080
      targetPort: 8080
  type: ClusterIP
```

## Security

### Wallet Security

- Private keys are encrypted using AES-GCM encryption
- Only authorized Discord users can interact with the bot
- Vote commands require a secret phrase

### Access Control

- Bot only responds to your specific Discord user ID
- All commands are logged for audit purposes
- Database stores voting history and proposal tracking

### Security Features

1. **Encrypted Storage**: Keys are encrypted using AES-GCM before storage
2. **Access Control**: Only authorized users can perform key operations
3. **Backup Creation**: Automatic backup creation with timestamps
4. **Validation**: Checks that keys exist and have correct addresses

### Best Practices

1. Use a dedicated server for running the bot
2. Keep your encryption key and vote secret secure
3. Regularly backup your wallet keys
4. Monitor the bot logs for suspicious activity
5. Use strong, unique secrets
6. Store key files in secure locations with restricted permissions
7. Regular encrypted backups of your keys
8. Limit access to the server running prop-voter
9. Monitor for unauthorized access attempts

## Troubleshooting

### Common Issues

1. **CLI tool not found**:

   - Check if auto-binary management is enabled
   - Manually update binary: `./prop-voter -binary update "Chain Name"`
   - Verify the `asset_pattern` matches available releases

2. **Wallet key not found**:

   - List keys to see what's available: `./prop-voter -key list`
   - Validate all keys: `./prop-voter -key validate`
   - Check specific key: `gaiad keys show my-key --address`

3. **Discord permissions**:

   - Ensure the bot has "Send Messages" and "Read Message History" permissions
   - Check that the bot is in the correct channel
   - Verify the bot is online (green status in Discord)

4. **RPC/REST endpoints**:

   - Test endpoints manually: `curl <rest-endpoint>/cosmos/gov/v1beta1/proposals?pagination.limit=1`
   - Try alternative public endpoints if needed
   - The scanner uses pagination to fetch only recent proposals

5. **Chain Registry issues**:
   - Network connectivity to Chain Registry: `curl -s https://raw.githubusercontent.com/cosmos/chain-registry/master/osmosis/chain.json`
   - Invalid chain names: Use exact Chain Registry identifiers (e.g., `osmosis`, not `Osmosis`)
   - Verify chain support: `./prop-voter -registry list`

### Binary Issues

```bash
# Check binary status
./prop-voter -binary list

# Manual update
./prop-voter -binary update "Chain Name"

# Validate installation
./prop-voter -validate
```

### Key Issues

```bash
# List keys to see what's available
./prop-voter -key list

# Validate all keys
./prop-voter -key validate

# Check specific key
gaiad keys show my-key --address
```

### Common Problems

1. **Binary Not Found**:
   - Chain Registry: Binaries are auto-discovered, no configuration needed
   - Legacy: Check the asset pattern matches available releases
2. **Key Import Fails**: Verify mnemonic format and chain CLI compatibility
3. **Permission Errors**: Ensure proper file permissions on bin and key directories
4. **Network Issues**: Check GitHub API access and Chain Registry connectivity
5. **Chain Registry Errors**:
   - Use exact chain names from the registry (case-sensitive)
   - Check network access to `https://raw.githubusercontent.com/cosmos/chain-registry/`
   - Verify chain exists: `./prop-voter -registry list`
6. **Governance API Errors**: The scanner fetches only the 25 most recent proposals to prevent API overload and compatibility issues with chains that have upgraded governance modules

### Logs

The application provides detailed logging. Run with `-debug` flag for verbose output:

```bash
./prop-voter -config config.yaml -debug
```

## Development

### Code Quality

```bash
# Format code
make fmt

# Install and run linter
make install-lint
make lint
```

### Building for Production

```bash
# Build for multiple platforms
make build-all

# Build with specific options
CGO_ENABLED=1 go build -ldflags="-s -w" -o prop-voter ./cmd/prop-voter
```

### Database

The application uses SQLite for data storage. The database includes:

- **Proposals**: Governance proposals from all chains
- **Votes**: Your voting history
- **Wallet Info**: Encrypted wallet information
- **Notification Logs**: Tracking of sent notifications

### File Structure

```
prop-voter/
├── bin/                    # Managed binaries
│   ├── gaiad
│   ├── osmosisd
│   └── junod
├── keys/                   # Key files for import
│   ├── validator-key.mnemonic
│   └── backup-key.txt
├── key-backups/           # Backup directory
│   └── key-backup-20231213-140530/
├── config.yaml           # Main configuration
├── prop-voter.db         # SQLite database
└── logs/                 # Application logs
```

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests
5. Submit a pull request

### Development Guidelines

- Follow Go best practices
- Add unit tests for new functionality
- Update documentation for new features
- Test with multiple chains before submitting

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Security Disclosure

If you discover a security vulnerability, please send an email to chalabi@jchalabi.xyz. Do not create a public issue.
