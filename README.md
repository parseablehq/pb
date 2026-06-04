# pb

[![Build](https://github.com/parseablehq/pb/actions/workflows/build.yaml/badge.svg)](https://github.com/parseablehq/pb/actions/workflows/build.yaml)
[![License: AGPL v3](https://img.shields.io/badge/license-AGPL--3.0-blue.svg)](LICENSE)
[![Latest Release](https://img.shields.io/github/v/release/parseablehq/pb)](https://github.com/parseablehq/pb/releases/latest)
[![Go Version](https://img.shields.io/github/go-mod/go-version/parseablehq/pb)](go.mod)

`pb` is the command line interface for [Parseable](https://github.com/parseablehq/parseable). Use it to query logs with SQL, run PromQL queries on metrics, stream live events, and manage datasets, users, and profiles from your terminal.

## Installation

Download the binary for your platform from the [releases page](https://github.com/parseablehq/pb/releases/latest).

**macOS and Linux**

Pick the binary for your platform:

| Platform | Binary |
|---|---|
| macOS Apple Silicon (M1, M2, M3) | `pb_darwin_arm64` |
| macOS Intel | `pb_darwin_amd64` |
| Linux x86 64-bit | `pb_linux_amd64` |
| Linux ARM 64-bit | `pb_linux_arm64` |

```bash
curl -LO https://github.com/parseablehq/pb/releases/latest/download/<binary>
chmod +x <binary>
sudo mv <binary> /usr/local/bin/pb
```

**Windows**

Download `pb_windows_amd64` from the [releases page](https://github.com/parseablehq/pb/releases/latest), rename it to `pb.exe`, and move it to a folder in your `PATH`.

**Using Go**

```bash
go install github.com/parseablehq/pb@latest
```

Verify the install:

```bash
pb --version
```

## Quick Start

**Step 1 — Connect to a server**

Run `pb login` to start the interactive setup wizard:

```bash
pb login
```

The wizard asks for your server URL, auth method (username/password or API key), and a profile name. Use arrow keys to navigate, Enter to confirm, and Esc to go back.

**Step 2 — Run your first query**

```bash
pb sql run "SELECT * FROM backend" --from=10m --to=now
```

## Commands

### Profiles

Profiles store your server connections. All commands use the active default profile automatically.

Config file location: `~/.config/pb/config.toml` on macOS/Linux, `%AppData%\pb\config.toml` on Windows.

```bash
pb login                                                          # interactive setup wizard
pb profile add staging https://staging.example.com admin pass    # add a profile without prompts
pb profile list                                                   # list all saved profiles
pb profile default staging                                        # switch the active profile
pb profile update staging https://new-host.example.com           # update a profile URL
pb profile remove staging                                         # remove a profile
pb logout                                                         # remove the active profile
```

### SQL Query

```bash
pb sql run "SELECT * FROM backend" --from=10m --to=now
pb sql run "SELECT * FROM backend" --from=1h
pb sql run "SELECT * FROM backend" --from=2024-01-01T00:00:00Z --to=2024-01-01T01:00:00Z
pb sql run "SELECT * FROM backend" --from=1h --output json | jq .
pb sql run "SELECT * FROM backend WHERE status = 500" --from=1h --save-as=server-errors
pb sql list    # list and apply saved queries
```

OTel fields with dots like `service.name` and `http.status_code` work directly in queries without manual quoting.

### SQL Interactive Mode

```bash
pb sql run -i
pb sql run "SELECT * FROM backend" --from=1h -i
```

Panels: Query, Time Range, Table. Navigate with Tab and Shift+Tab.

### PromQL Query

```bash
pb promql run "rate(http_requests_total[5m])" --dataset otel_metrics --from=1h --step=1m
pb promql run "up" --dataset otel_metrics --instant
pb promql run "http_requests_total" --dataset otel_metrics --output json
```

Explore labels and series:

```bash
pb promql labels --dataset otel_metrics
pb promql label-values job --dataset otel_metrics
pb promql series --match 'http_requests_total{job="api"}' --dataset otel_metrics
```

Cardinality analysis:

```bash
pb promql cardinality label-names --dataset otel_metrics
pb promql active-queries
```

### PromQL Interactive Mode

```bash
pb promql run -i
pb promql run "http_requests_total" --dataset otel_metrics --from=1h -i
```

Panels: Dataset, Query, Time, Step, Table. Navigate with Tab and Shift+Tab. Press Space on the Step panel to toggle between range and instant mode.

### Live Tail

Stream events from a dataset in real time:

```bash
pb tail backend
pb tail backend | jq 'select(.level == "error")'
pb tail backend | grep timeout
```

Press Ctrl+C to stop.

Note: `pb tail` uses gRPC on port 8001. Make sure that port is open on your server in addition to the main HTTP port.

### Dataset Management

```bash
pb dataset list
pb dataset info my_logs
pb dataset add my_logs
pb dataset remove my_logs
```

### Users and Roles

```bash
pb user list
pb user add alice
pb user set-role alice admin,editor
pb user remove alice

pb role list
pb role add ingestors
pb role remove ingestors
```

### Other Commands

```bash
pb status       # check connection and server version
pb version      # print client and server version info
```

### Shell Autocomplete

**Bash**
```bash
pb autocomplete bash > /etc/bash_completion.d/pb
source /etc/bash_completion.d/pb
```

**Zsh**
```bash
pb autocomplete zsh > /usr/local/share/zsh/site-functions/_pb
autoload -Uz compinit && compinit
```

**PowerShell**
```powershell
pb autocomplete powershell > $env:USERPROFILE\Documents\PowerShell\pb_complete.ps1
. $env:USERPROFILE\Documents\PowerShell\pb_complete.ps1
```

## Contributing

Contributions are welcome. Please read [CONTRIBUTING.md](CONTRIBUTING.md) for setup instructions, branch naming conventions, and the PR checklist. All contributors must sign the CLA — the bot will prompt you on your first PR.

## License

`pb` is released under the [GNU Affero General Public License v3.0](LICENSE).
