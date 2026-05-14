# Contributing to pb

Thank you for your interest in contributing to `pb`! This document covers everything you need to get started.

## Prerequisites

- [Go 1.25+](https://go.dev/dl/)
- `make`
- A running [Parseable Server](https://github.com/parseablehq/parseable) for integration testing

## Development Setup

```bash
# Clone the repo
git clone https://github.com/parseablehq/pb.git
cd pb

# Install lint tooling
make getdeps

# Build the binary
make build          # produces ./pb

# Or install to $GOPATH/bin
make install
```

## Running Tests

```bash
go test ./...
```

## Running the Linter

```bash
make lint           # golangci-lint
make vet            # go vet
make verifiers      # vet + lint (full check, same as CI)
```

All checks must pass before raising a PR.

## Making Changes

### Branch Naming

| Type | Pattern | Example |
|------|---------|---------|
| Feature | `feat/<short-desc>` | `feat/promql-instant-query` |
| Bug fix | `fix/<short-desc>` | `fix/double-slash-url` |
| Docs | `docs/<short-desc>` | `docs/update-readme` |
| Chore | `chore/<short-desc>` | `chore/upgrade-go-version` |

### CLA

All contributors must sign the [Contributor License Agreement](https://github.com/parseablehq/.github) before a PR can be merged. The CLA bot will prompt you automatically on your first PR.

## Pull Request Checklist

Before marking a PR as ready for review:

- [ ] `make verifiers` passes locally
- [ ] New behavior is covered by tests where applicable
- [ ] README or docs updated if a user-visible change was made
- [ ] CLA signed (bot will comment if not)

## Code Style

- `gofmt` / `gofumpt` formatting (enforced by CI)
- `goimports` for import ordering
- Prefer short, focused functions

## Getting Help

Open a [GitHub Discussion](https://github.com/parseablehq/pb/discussions) for questions, or join the [Parseable Slack](https://launchpass.com/parseable).

Please read our [Code of Conduct](CODE_OF_CONDUCT.md) before participating.
