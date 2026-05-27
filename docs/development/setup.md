# Development Setup

## Intended Audience

This documentation is for people who develop, maintain, or verify the
`local-irsa` repository.

User workflows and command reference are in the manual:

- [Overview](../manual/overview.md)
- [Quick Start](../manual/quick-start.md)
- [`init`](../manual/commands/init.md)
- [`install`](../manual/commands/install.md)
- [`bind`](../manual/commands/bind.md)
- [`doctor`](../manual/commands/doctor.md)

Do not put user command reference, release work, or design reconciliation notes
in this development documentation.

## Development Environment

Use these tools for local development:

- Go 1.26 or later. This repository uses Go 1.26.2 in `go.mod` and devbox.
- `devbox`.
- Taskfile.
- `kind`.
- `kubectl`.
- a Docker-compatible container runtime.

`task up` downloads the cert-manager manifest from GitHub Releases, so the
development machine needs network access to GitHub Releases.

When using devbox, check the Go version with:

```text
devbox run go version
```

## CLI Installation and Local Runs

The user-facing setup method is a Go tool dependency pinned in the user's
project `go.mod`:

```text
go get -tool github.com/appthrust/local-irsa/cmd/local-irsa@<version>
go tool local-irsa --help
```

Use a concrete `<version>` instead of `@latest` when documenting repeatable
user setup.

Users who intentionally want a global personal binary can still use
`go install`, but that is not the main documentation path:

```text
go install github.com/appthrust/local-irsa/cmd/local-irsa@<version>
```

To install a development build from a checkout, run this at the repository
root:

```text
go install ./cmd/local-irsa
```

For one-time local runs during development, use:

```text
go run ./cmd/local-irsa <subcommand> ...
```

Release binaries, package managers, and container images are not defined yet.
