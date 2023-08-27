## pb

pb is the command line interface for [Parseable Server](https://github.com/parseablehq/parseable). pb allows you to manage Streams, Users, and Data on Parseable Server. You can use pb to manage multiple Parseable Server instances using Profiles.

We believe dashboard fatigue is one of key reasons for poor adoption of logging tools among developers. With pb, we intend to bring the familiar command line interface for querying and analyzing log data at scale.

![pb banner](https://github.com/parseablehq/.github/blob/main/images/pb/pb.png?raw=true)

### Installation

pb is available as a single, self contained binary for Mac, Linux, and Windows. You can download the latest version from the [releases page](https://github.com/parseablehq/pb/releases/latest).

To install pb, download the binary for your platform and place it in your `$PATH`. For example, on Linux:

```bash
wget https://github.com/parseablehq/pb/releases/download/v0.1.0/pb_linux_amd64 -O pb
chmod +x pb && mv pb /usr/local/bin
```

### Usage

pb comes configured with `demo` profile as the default. This means you can directly start using pb against the [demo Parseable Server](https://demo.parseable.io). For example, to query the stream `backend` on demo server, run:

```bash
pb query backend
```

#### Profiles

To start using pb against your Parseable server, you need to create a profile (a profile is a set of credentials for a Parseable Server instance). You can create a profile using the `pb profile create` command. For example:

```bash
pb profile add local http://localhost:8000 admin admin
```

This will create a profile named `local` that points to the Parseable Server at `http://localhost:8000` and uses the username `admin` and password `admin`.

You can create as many profiles as you like. To avoid having to specify the profile name every time you run a command, pb allows setting a default profile. To set the default profile, use the `pb profile default` command. For example:

```bash
pb profile default local
```

![pb profiles](https://github.com/parseablehq/.github/blob/main/images/pb/profile.png?raw=true)

#### Query

To query a stream, run:

```bash
pb query <stream-name>
```

![pb query](https://github.com/parseablehq/.github/blob/main/images/pb/query.png?raw=true)

#### Streams

Once a profile is configured, you can use pb to query and manage _that_ Parseable Server instance. For example, to list all the streams on the server, run:

```bash
pb stream list
```

![pb streams](https://github.com/parseablehq/.github/blob/main/images/pb/stream.png?raw=true)

#### Users

To list all the users with their privileges, run:

```bash
pb user list
```

You can also use the `pb users` command to manage users.

![pb users](https://github.com/parseablehq/.github/blob/main/images/pb/user.png?raw=true)

#### Version

Version command prints the version of pb and the Parseable Server it is configured to use.

```bash
pb version
```

![pb version](https://github.com/parseablehq/.github/blob/main/images/pb/version.png?raw=true)

#### Help

To get help on a command, run:

```bash
pb help <command>
```

![pb help](https://github.com/parseablehq/.github/blob/main/images/pb/help.png?raw=true)
