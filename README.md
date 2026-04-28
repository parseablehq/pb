# pb

Dashboard fatigue is one of key reasons for poor adoption of logging tools among developers. With pb, we intend to bring the familiar command line interface for querying and analyzing log data at scale.

pb is the command line interface for [Parseable Server](https://github.com/parseablehq/parseable). pb allows you to manage Streams, Users, and Data on Parseable Server. You can use pb to manage multiple Parseable Server instances using Profiles.

![pb](https://github.com/parseablehq/.github/blob/main/images/pb/pb.gif?raw=true)

## Installation

pb is available as a single, self contained binary for Mac, Linux, and Windows. You can download the latest version from the [releases page](https://github.com/parseablehq/pb/releases/latest).

To install pb, download the binary for your platform, un-tar the binary and place it in your `$PATH`.

## Usage

pb is configured with `demo` profile as the default. This means you can directly start using pb against the [demo Parseable Server](https://demo.parseable.com).

### Profiles

To start using pb against your Parseable server, create a profile (a profile is a set of credentials for a Parseable Server instance). You can create a profile using the `pb profile add` command. For example:

```bash
pb profile add local http://localhost:8000 admin admin
```

This will create a profile named `local` that points to the Parseable Server at `http://localhost:8000` and uses the username `admin` and password `admin`.

You can create as many profiles as you like. To avoid having to specify the profile name every time you run a command, pb allows setting a default profile. To set the default profile, use the `pb profile default` command. For example:

```bash
pb profile default local
```

### Query

By default `pb` sends json data to stdout.

```bash
pb query run "select * from backend" --from=1m --to=now
```

or specifying time range in rfc3999

```bash
pb query run "select * from backend" --from=2024-01-00T01:40:00.000Z --to=2024-01-00T01:55:00.000Z
```

You can use tools like `jq` and `grep` to further process and filter the output. Some examples:

```bash
pb query run "select * from backend" --from=1m --to=now | jq .
pb query run "select host, id, method, status from backend where status = 500" --from=1m --to=now | jq . > 500.json
pb query run "select host, id, method, status from backend where status = 500" | jq '. | map(select(.method == "PATCH"))'
pb query run "select host, id, method, status from backend where status = 500" --from=1m --to=now | grep "POST" | jq . | less
```

#### Save Filter

To save a query as a filter use the `--save-as` flag followed by a name for the filter. For example:

```bash
pb query run "select * from backend" --from=1m --to=now --save-as=FilterName
```

### List Filter

To list all filter for the active user run:

```bash
pb query list
```

### Live Tail

`pb` can be used to tail live data from Parseable Server. To tail live data, use the `pb tail` command. For example:

```bash
pb tail backend
```

You can also use the terminal tools like `jq` and `grep` to filter and process the tail output. Some examples:

```bash
pb tail backend | jq '. | select(.method == "PATCH")'
pb tail backend | grep "POST" | jq .
```

To stop tailing, press `Ctrl+C`.

### Stream Management

Once a profile is configured, you can use pb to query and manage _that_ Parseable Server instance. For example, to list all the streams on the server, run:

```bash
pb stream list
```

### Users

To list all the users with their privileges, run:

```bash
pb user list
```

You can also use the `pb users` command to manage users.

### Version

Version command prints the version of pb and the Parseable Server it is configured to use.

```bash
pb version
```

### Add Autocomplete

To enable autocomplete for pb, run the following command according to your shell:

For bash:

```bash
pb autocomplete bash > /etc/bash_completion.d/pb
source /etc/bash_completion.d/pb
```

For zsh:

```zsh
pb autocomplete zsh > /usr/local/share/zsh/site-functions/_pb
autoload -U compinit && compinit
```

For powershell

```powershell
pb autocomplete powershell > $env:USERPROFILE\Documents\PowerShell\pb_complete.ps1
. $PROFILE
```
