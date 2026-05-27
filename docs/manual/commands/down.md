# `down`

## Purpose

`down` removes local-irsa managed AWS resources and ServiceAccount annotations
for one cluster. It does not delete the `kind` cluster.

## Prerequisites

Use the same local-irsa state and AWS account that created the resources.

## Command

```text
go tool local-irsa down --name NAME [--context CONTEXT] [--profile PROFILE] [--delete-bucket] [--yes]
```

## Required Flags

- `--name`

Without `--yes`, `down` asks for confirmation before deleting resources.

## Resources

`down` removes ServiceAccount annotations for bindings in state, detaches
managed policies, deletes local-irsa owned IAM Roles, deletes the IAM OIDC
Provider, and deletes issuer objects from S3.

Without `--delete-bucket`, the S3 bucket and local state remain. You can run
`go tool local-irsa down --name NAME --delete-bucket` later to delete the bucket too.

With `--delete-bucket`, the bucket is deleted only after managed resources are
removed. If the IAM OIDC Provider cannot be deleted, the bucket is kept so that
the issuer bucket name is not released while a provider still trusts that
issuer URL.

When `--delete-bucket` is set and all managed cleanup succeeds, local state is
also deleted.

## Next Step

Delete the `kind` cluster yourself when you no longer need it. If you used the
demo policy and it still exists, run [`demo delete-policy`](demo.md) after
policy attachments are removed.

## Main Failure Conditions

- The state does not exist.
- The confirmation prompt is not accepted.
- An AWS resource is not owned by the current local-irsa cluster.
- The IAM OIDC Provider cannot be deleted.
