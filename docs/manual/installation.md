# Installation

Add `local-irsa` to your project's Go tool dependencies with a release tag.

```text
go get -tool github.com/appthrust/local-irsa/cmd/local-irsa@<version>
go tool local-irsa --help
```

Use a concrete `<version>` instead of `@latest` so that another setup can use
the same build. This records the tool version in your project's `go.mod`.

`go tool` requires a Go module. If you are not already in one, create a small
working directory first.

```text
mkdir local-irsa-work
cd local-irsa-work
go mod init local-irsa-work
go get -tool github.com/appthrust/local-irsa/cmd/local-irsa@<version>
go tool local-irsa --help
```

The standalone binary name is still `local-irsa`. If you intentionally want a
global personal binary outside a project `go.mod`, you can install one with
`go install`.

```text
go install github.com/appthrust/local-irsa/cmd/local-irsa@<version>
```

`go install` writes the binary to `$GOBIN`, or to `$GOPATH/bin` when `$GOBIN`
is not set. Add that directory to `PATH` before running the standalone
`local-irsa` binary.

## Prerequisites

Prepare these tools and permissions before you start:

- `kind`;
- `kubectl`;
- a Docker-compatible container runtime;
- AWS credentials;
- AWS permissions for IAM, S3, and STS;
- an AWS managed policy or customer managed policy for `bind`.

If you install the webhook, the target cluster must already have cert-manager.
`go tool local-irsa install` does not install cert-manager. Use
`go tool local-irsa install --skip-webhook` when you only want the S3 issuer
and IAM OIDC Provider.

The AWS CLI is not required by `local-irsa`. AWS access is resolved through the
AWS SDK for Go v2.

Next: [Quick Start](quick-start.md).
