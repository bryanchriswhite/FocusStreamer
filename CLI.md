# FocusStreamer CLI Reference

FocusStreamer provides a comprehensive command-line interface built with Cobra and Viper for easy configuration and management.

## Table of Contents

- [Installation](#installation)
- [Global Flags](#global-flags)
- [Commands](#commands)
  - [serve](#serve)
  - [config](#config)
  - [list](#list)
  - [allowlist](#allowlist)
  - [pattern](#pattern)
- [Configuration File](#configuration-file)
- [Examples](#examples)

## Installation

```bash
# Build from source
make build

# Install to PATH
sudo cp build/focusstreamer /usr/local/bin/

# Or run directly
./build/focusstreamer [command]
```

## Global Flags

These flags are available for all commands:

| Flag | Description | Default |
|------|-------------|---------|
| `--config` | Path to config file | `$HOME/.config/focusstreamer/config.yaml` |
| `--port` | Server port | `8080` |
| `--log-level` | Log level (debug, info, warn, error) | `info` |
| `-h, --help` | Help for any command | - |

## Commands

### serve

Start the FocusStreamer HTTP server with X11 window monitoring.

```bash
focusstreamer serve [flags]
```

**Flags:**
- Inherits all global flags

**Examples:**

```bash
# Start server on default port (8080)
focusstreamer serve

# Start server on custom port
focusstreamer serve --port 9090

# Start with specific config file
focusstreamer serve --config /path/to/config.yaml

# Start with debug logging
focusstreamer serve --log-level debug
```

---

### config

Manage FocusStreamer configuration settings.

#### config show

Display the current configuration.

```bash
focusstreamer config show [flags]
```

**Flags:**
- `-f, --format` - Output format: `yaml` or `json` (default: `yaml`)

**Examples:**

```bash
# Show configuration as YAML
focusstreamer config show

# Show configuration as JSON
focusstreamer config show --format json
```

#### config get

Get a specific configuration value.

```bash
focusstreamer config get KEY
```

**Examples:**

```bash
# Get server port
focusstreamer config get server_port

# Get log level
focusstreamer config get log_level

# Get virtual display width
focusstreamer config get virtual_display.width
```

#### config set

Set a specific configuration value.

```bash
focusstreamer config set KEY VALUE
```

**Examples:**

```bash
# Set server port
focusstreamer config set server_port 9090

# Set log level
focusstreamer config set log_level debug

# Set virtual display dimensions
focusstreamer config set virtual_display.width 2560
focusstreamer config set virtual_display.height 1440
```

#### config path

Show the path to the configuration file.

```bash
focusstreamer config path
```

---

### list

List running applications detected by FocusStreamer.

```bash
focusstreamer list [flags]
```

**Flags:**
- `-f, --format` - Output format: `table` or `json` (default: `table`)
- `-w, --allowlisted` - Show only allowlisted applications
- `-c, --current` - Show current focused window

**Examples:**

```bash
# List all applications in table format
focusstreamer list

# List applications in JSON format
focusstreamer list --format json

# List only allowlisted applications
focusstreamer list --allowlisted

# Show currently focused window
focusstreamer list --current

# Show current window as JSON
focusstreamer list --current --format json
```

**Output (table format):**
```
NAME                  CLASS                 PID      WHITELISTED
----                  -----                 ---      -----------
Firefox               firefox               12345    Yes
Terminal              gnome-terminal        12346    No
Code                  code                  12347    Yes
```

---

### allowlist

Manage application allowlist.

#### allowlist add

Add an application to the allowlist by its window class.

```bash
focusstreamer allowlist add CLASS
```

**Examples:**

```bash
# Add Firefox to allowlist
focusstreamer allowlist add firefox

# Add terminal to allowlist
focusstreamer allowlist add gnome-terminal-server

# Add VS Code to allowlist
focusstreamer allowlist add code
```

#### allowlist remove

Remove an application from the allowlist.

```bash
focusstreamer allowlist remove CLASS
```

**Examples:**

```bash
# Remove Firefox from allowlist
focusstreamer allowlist remove firefox
```

#### allowlist list

Display all allowlisted applications and patterns.

```bash
focusstreamer allowlist list
```

**Output:**
```
Allowlisted Applications:
  • firefox
  • code
  • gnome-terminal-server

Allowlist Patterns:
  • .*Terminal.*
  • .*Code.*
```

---

### pattern

Manage regex patterns for auto-allowlisting applications.

Patterns are matched against both window class and window title.

#### pattern add

Add a regex pattern for auto-allowlisting.

```bash
focusstreamer pattern add PATTERN
```

**Examples:**

```bash
# Match all terminal applications
focusstreamer pattern add ".*[Tt]erminal.*"

# Match all applications with "Code" in the name
focusstreamer pattern add ".*Code.*"

# Match Firefox specifically
focusstreamer pattern add "^firefox$"

# Match any browser
focusstreamer pattern add ".*(firefox|chrome|chromium).*"
```

#### pattern remove

Remove a regex pattern from auto-allowlisting.

```bash
focusstreamer pattern remove PATTERN
```

**Examples:**

```bash
# Remove terminal pattern
focusstreamer pattern remove ".*Terminal.*"
```

#### pattern list

Display all configured allowlist patterns.

```bash
focusstreamer pattern list
```

**Output:**
```
Allowlist Patterns:
  1. .*Terminal.*
  2. .*Code.*
  3. ^firefox$
```

---

## Configuration File

FocusStreamer uses YAML for configuration (previously JSON). The default location is:

```
$HOME/.config/focusstreamer/config.yaml
```

### Configuration Structure

```yaml
server_port: 8080
log_level: info

allowlist_patterns:
  - ".*Terminal.*"
  - ".*Code.*"

allowlisted_apps:
  firefox: true
  code: true
  gnome-terminal-server: true

virtual_display:
  width: 1920
  height: 1080
  refresh_hz: 60
  enabled: true
```

### Configuration Keys

| Key | Type | Description | Default |
|-----|------|-------------|---------|
| `server_port` | int | HTTP server port | `8080` |
| `log_level` | string | Logging level | `info` |
| `allowlist_patterns` | []string | Regex patterns for auto-allowlist | `[]` |
| `allowlisted_apps` | map | Explicitly allowlisted apps | `{}` |
| `virtual_display.width` | int | Virtual display width | `1920` |
| `virtual_display.height` | int | Virtual display height | `1080` |
| `virtual_display.refresh_hz` | int | Virtual display refresh rate | `60` |
| `virtual_display.enabled` | bool | Enable virtual display | `true` |

---

## Examples

### Complete Workflow Example

```bash
# 1. Build the application
make build

# 2. Check available applications
./build/focusstreamer list

# 3. Add applications to allowlist
./build/focusstreamer allowlist add firefox
./build/focusstreamer allowlist add code

# 4. Add a pattern for all terminal apps
./build/focusstreamer pattern add ".*Terminal.*"

# 5. View current configuration
./build/focusstreamer config show

# 6. Check what's allowlisted
./build/focusstreamer allowlist list

# 7. Start the server
./build/focusstreamer serve

# 8. In another terminal, see what's currently focused
./build/focusstreamer list --current
```

### Port Configuration Example

```bash
# Check current port
focusstreamer config get server_port

# Change port to 9090
focusstreamer config set server_port 9090

# Start server on new port
focusstreamer serve

# Or override port without saving
focusstreamer serve --port 8888
```

### Pattern Matching Examples

```bash
# Match any application with "Terminal" in the name (case-sensitive)
focusstreamer pattern add ".*Terminal.*"

# Match any application with "terminal" (case-insensitive)
focusstreamer pattern add "(?i).*terminal.*"

# Match specific applications
focusstreamer pattern add "^(firefox|chrome|chromium)$"

# Match all Code editors (VS Code, VSCodium, etc.)
focusstreamer pattern add ".*[Cc]ode.*"
```

### JSON Output for Scripting

```bash
# Get all applications as JSON
focusstreamer list --format json | jq '.[] | select(.allowlisted == true)'

# Get current window info as JSON
focusstreamer list --current --format json | jq '.class'

# Export configuration
focusstreamer config show --format json > backup-config.json
```

### Development Workflow

```bash
# Start server with debug logging
focusstreamer serve --log-level debug --port 8080

# In another terminal, monitor current window
watch -n 1 'focusstreamer list --current'

# Test pattern matching
focusstreamer pattern add ".*Test.*"
focusstreamer list --allowlisted
```

---

## Tips and Tricks

### Finding Window Classes

To find the window class of an application:

1. Run `focusstreamer list` to see all windows
2. Or use `xprop` utility:
   ```bash
   xprop | grep WM_CLASS
   # Then click on the window
   ```

### Bulk Operations

```bash
# Add multiple applications
for app in firefox chrome code terminal; do
    focusstreamer allowlist add $app
done

# Remove all patterns and start fresh
focusstreamer config show --format json | \
    jq -r '.allowlist_patterns[]' | \
    while read pattern; do
        focusstreamer pattern remove "$pattern"
    done
```

### Configuration Backup

```bash
# Backup configuration
cp ~/.config/focusstreamer/config.yaml ~/.config/focusstreamer/config.yaml.backup

# Restore configuration
cp ~/.config/focusstreamer/config.yaml.backup ~/.config/focusstreamer/config.yaml
```

### Using Custom Config Locations

```bash
# Use project-specific config
focusstreamer serve --config ./project-focusstreamer.yaml

# Use system-wide config
sudo focusstreamer serve --config /etc/focusstreamer/config.yaml
```

---

## Troubleshooting

### Command Not Found

```bash
# Make sure binary is in PATH
export PATH=$PATH:$(pwd)/build

# Or create a symlink
sudo ln -s $(pwd)/build/focusstreamer /usr/local/bin/focusstreamer
```

### Configuration Not Saving

```bash
# Check config file permissions
ls -la ~/.config/focusstreamer/config.yaml

# Check config file path
focusstreamer config path
```

### X11 Connection Issues

```bash
# Check DISPLAY variable
echo $DISPLAY

# Ensure X11 is accessible
xdpyinfo

# If using SSH, enable X11 forwarding
ssh -X user@host
```

### Port Already in Use

```bash
# Find what's using the port
lsof -i :8080

# Use a different port
focusstreamer serve --port 8081

# Or update config
focusstreamer config set server_port 8081
```

---

## See Also

- [README.md](README.md) - Project overview and quick start
- [ARCHITECTURE.md](ARCHITECTURE.md) - Detailed architecture documentation
- [TESTING.md](TESTING.md) - Testing guide
- [CONTRIBUTING.md](CONTRIBUTING.md) - Contribution guidelines
