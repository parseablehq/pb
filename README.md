# pb

[![Build](https://github.com/parseablehq/pb/actions/workflows/build.yaml/badge.svg)](https://github.com/parseablehq/pb/actions/workflows/build.yaml)
[![License: AGPL v3](https://img.shields.io/badge/license-AGPL--3.0-blue.svg)](LICENSE)
[![Latest Release](https://img.shields.io/github/v/release/parseablehq/pb)](https://github.com/parseablehq/pb/releases/latest)
[![Go Version](https://img.shields.io/github/go-mod/go-version/parseablehq/pb)](go.mod)

`pb` is the command line interface for [Parseable](https://github.com/parseablehq/parseable) — a fast, lightweight log and metrics storage server. Use `pb` to run SQL and PromQL queries, tail live data, manage datasets, users, and profiles, all from your terminal.

## Installation

Download the latest binary for your platform from the [releases page](https://github.com/parseablehq/pb/releases/latest).

**macOS / Linux**

```bash
tar -xzf pb_<version>_<os>_<arch>.tar.gz
mv pb /usr/local/bin/pb
pb --version
```

**Windows**

1. Download `pb_<version>_windows_amd64.tar.gz` from the releases page
2. Open PowerShell and extract:

```powershell
tar -xzf pb_<version>_windows_amd64.tar.gz
```

3. Move `pb.exe` to a folder in your `PATH` (e.g. `C:\Users\<you>\bin\`) and verify:

```powershell
pb --version
```

**Install with Go (all platforms)**

```bash
go install github.com/parseablehq/pb@latest
```

## Quick Start

### Step 1 — Connect to a server

Run `pb login` to launch the interactive setup wizard:

```bash
pb login
```

The wizard walks you through:
- **Choose type** — Self-hosted or Parseable Cloud
- **Enter server URL** — e.g. `http://localhost:8000`
- **Choose auth** — Username & Password, or Token
- **Enter credentials**
- **Name the profile** — e.g. `local`, `staging`, `prod`

Use `↑ ↓` to navigate lists, `Enter` to confirm, `Esc` to go back one step. If a profile name already exists, the wizard asks whether to replace it or pick a new name.

> **Prefer a one-liner?** Use `pb profile add` instead — see [Profiles](#profiles).

### Step 2 — Run your first query

```bash
pb query run "SELECT * FROM backend" --from=10m --to=now
```

That's it. See the sections below for every available command.

---

## Commands

### Profiles

Manage multiple Parseable server connections. All commands use the active default profile automatically.

Profiles are stored in `~/.config/pb/config.toml` (macOS/Linux) or `%AppData%\pb\config.toml` (Windows).

```bash
pb login                                                            # interactive setup wizard (recommended for new users)
pb profile add staging https://staging.example.com admin secret    # add a profile non-interactively
pb profile list                                                     # list all profiles
pb profile default staging                                          # switch default profile
pb profile update staging https://new-host.example.com:8000        # update URL for a profile
pb profile remove staging                                           # remove a profile
pb logout                                                           # remove the active profile
```

When you remove the default profile:
- 1 profile remaining → it becomes the new default automatically
- 2+ remaining → an interactive picker lets you choose the new default
- 0 remaining → default is cleared

### SQL Query

Query a dataset and print results to stdout.

```bash
pb query run "SELECT * FROM backend" --from=10m --to=now
```

**Time range** — supports relative durations, day shorthand, and RFC3339:

```bash
pb query run "SELECT * FROM backend" --from=1h                           # last 1 hour
pb query run "SELECT * FROM backend" --from=7d                           # last 7 days
pb query run "SELECT * FROM backend" \
  --from=2024-01-01T00:00:00Z --to=2024-01-01T01:00:00Z                  # exact window
```

**JSON output:**

```bash
pb query run "SELECT * FROM backend" --from=1h --output json | jq .
```

**Interactive table view** — navigate, filter, and paginate results in the terminal:

```bash
pb query run "SELECT * FROM backend" --from=1h -i
```

**Save a query for later:**

```bash
pb query run "SELECT * FROM backend WHERE status = 500" --from=1h --save-as=server-errors
pb query list    # list and apply saved queries
```

> **Note on field names with dots** — OTel fields like `service.name`, `http.status_code`, and `code.file.path` can be used directly in queries without manual quoting. `pb` handles the quoting transparently:
> ```bash
> pb query run "SELECT * FROM otel-logs WHERE service.name = 'frontend'" --from=1h
> ```

#### Interactive Mode Keys

| Key | Action |
|-----|--------|
| `Tab` | Next panel (Query → Time → Table) |
| `Shift+Tab` | Previous panel |
| `Enter` (Time panel) | Open time range picker |
| `Ctrl+R` | Run query |
| `Ctrl+B` | Fetch previous page |
| `Ctrl+C` | Exit |

**Table panel keys:**

| Key | Action |
|-----|--------|
| `↑` / `w` | Scroll up |
| `↓` / `s` | Scroll down |
| `Shift+↑` / `PgUp` | Previous page |
| `Shift+↓` / `PgDn` | Next page |
| `←` / `a` | Scroll columns left |
| `→` / `d` | Scroll columns right |
| `/` | Filter rows |
| `Esc` | Clear filter |

### PromQL Query

Query metrics datasets using PromQL expressions.

```bash
# Range query — returns a time series over the given window
pb query promql run "rate(http_requests_total[5m])" \
  --dataset otel_metrics --from=1h --step=1m

# Instant query — evaluate at a single point in time
pb query promql run "up" --dataset otel_metrics --instant

# JSON output
pb query promql run "http_requests_total" --dataset otel_metrics -o json
```

**Explore metrics:**

```bash
pb query promql labels --dataset otel_metrics                                           # all label names
pb query promql label-values job --dataset otel_metrics                                 # values for a label
pb query promql series --match 'http_requests_total{job="api"}' --dataset otel_metrics  # matching series
```

**Cardinality analysis** — find high-cardinality labels before they cause memory issues:

```bash
pb query promql cardinality label-names --dataset otel_metrics           # labels by distinct value count
pb query promql cardinality label-values --label service.name \
  --dataset otel_metrics                                                  # series count per label value
pb query promql cardinality active-series --dataset otel_metrics         # total active series
```

**TSDB statistics:**

```bash
pb query promql tsdb --dataset otel_metrics
```

**Currently running queries:**

```bash
pb query promql active-queries
```

### Live Tail

Stream live log events from a dataset as they arrive:

```bash
pb tail backend
```

Filter in real time with standard tools:

```bash
pb tail backend | jq '.[] | select(.method == "PATCH")'
pb tail backend | grep "POST" | jq .
```

Press `Ctrl+C` to stop.

### Dataset Management

```bash
pb dataset list                  # list all datasets on the server
pb dataset info my_logs          # show stats (size, event count) for a dataset
pb dataset add my_logs           # create a new dataset
pb dataset remove my_logs        # delete a dataset
```

### Users and Roles

```bash
pb user list                              # list all users
pb user add alice                         # create a user (prompts for password)
pb user set-role alice admin,editor       # assign roles
pb user remove alice                      # delete a user

pb role list                              # list available roles
pb role add ingestors                     # create a role (interactive privilege picker)
pb role remove ingestors                  # delete a role
```

### Status

Check connectivity and version info for the active server:

```bash
pb status
```

### Version

```bash
pb version
pb --version
```

### Autocomplete

Enable shell completion for `pb` commands and flags.

**Bash:**

```bash
pb autocomplete bash > /etc/bash_completion.d/pb
source /etc/bash_completion.d/pb
```

**Zsh:**

```zsh
pb autocomplete zsh > /usr/local/share/zsh/site-functions/_pb
autoload -U compinit && compinit
```

**PowerShell:**

```powershell
pb autocomplete powershell > $env:USERPROFILE\Documents\PowerShell\pb_complete.ps1
. $PROFILE
```

---

## Contributing

Contributions are welcome! Please read [CONTRIBUTING.md](CONTRIBUTING.md) for how to set up your dev environment, branch naming conventions, and the PR checklist. All contributors must sign the CLA — the bot will prompt you automatically on your first PR.

## License

`pb` is released under the [GNU Affero General Public License v3.0](LICENSE).
