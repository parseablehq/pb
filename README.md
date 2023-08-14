## PB

PB (short for Parseable) is a command line interface for [Parseable Server](https://github.com/parseablehq/parseable). PB allows you to manage Streams, Users, and Data on Parseable Server. You can use PB to manage multiple Parseable Server instances using Profiles.

### Installation

PB is available as a single, self contained binary for Mac, Linux, and Windows. You can download the latest version from the [releases page](https://github.com/parseablehq/pb/releases/latest).

To install PB, download the binary for your platform and place it in a directory that is in your $PATH. For example, on Mac you can place the binary in `/usr/local/bin`.

![pb query](https://github.com/parseablehq/.github/blob/main/images/pb.png?raw=true)

### Usage

To get started, `pb` needs at least one profile (a profile is a set of credentials for a Parseable Server instance). You can create a profile using the `pb profile create` command. For example:

```bash
pb profile add demo https://demo.parseable.io admin admin
```

This will create a profile named `demo` that points to the Parseable Server instance at `https://demo.parseable.io` and uses the username `admin` and password `admin`. You can create as many profiles as you like. To avoid having to specify the profile name every time you run a command, `pb` allows setting a default profile. To set the default profile, use the `pb profile default` command. For example:

```bash
pb profile default demo
```

Now you can use `pb` to query and manage your Parseable Server instance. For example, to list all the streams on the server, run:

```bash
pb stream list
```

To query a stream, run:

```bash
pb query <stream-name>
```

To get help on a command, run:

```bash
pb help <command>
```
