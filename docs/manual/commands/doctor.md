# `doctor`

## Purpose

`doctor` checks that local-irsa state, cluster OIDC settings, S3 issuer
documents, and the IAM OIDC Provider match. When you pass a ServiceAccount, it
also checks token exchange with AWS STS.

## Prerequisites

Run `install` first. To check a ServiceAccount, run `bind` first.

## Command

```text
go tool local-irsa doctor --name NAME [--namespace NS --service-account SA] [--context CONTEXT] [--profile PROFILE]
```

## Required Flags

- `--name`

`--namespace` and `--service-account` must be used together. Passing only one
of them is an error.

## Resources

Without a ServiceAccount, `doctor` checks cluster OIDC settings, the S3 issuer
objects, and the IAM OIDC Provider.

With a ServiceAccount, `doctor` also checks the ServiceAccount role ARN,
creates a short-lived token, and calls `sts:AssumeRoleWithWebIdentity`.

In normal output, `doctor` prints a `State:` block after reading state. The
block includes the state file path and pretty JSON from `state.json`. Use
`--quiet` to hide this block.

## Next Step

Fix any reported mismatch, then run `doctor` again. If the cluster was
recreated, run [`install`](install.md) again before rerunning `doctor`.

## Main Failure Conditions

- Local state does not exist.
- The cluster OIDC issuer or JWKS URL differs from state.
- S3 issuer documents do not match the current cluster.
- The IAM OIDC Provider does not match local-irsa state.
- The ServiceAccount binding is missing or token exchange fails.
