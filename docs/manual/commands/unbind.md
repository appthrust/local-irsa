# `unbind`

## Purpose

`unbind` removes one ServiceAccount binding. It does not delete the S3 issuer,
IAM OIDC Provider, S3 bucket, or webhook.

## Prerequisites

The target binding must exist in local-irsa state.

## Command

```text
go tool local-irsa unbind --name NAME --namespace NS --service-account SA [--context CONTEXT] [--profile PROFILE]
```

## Required Flags

- `--name`
- `--namespace`
- `--service-account`

## Resources

`unbind` removes local-irsa annotations from the ServiceAccount, detaches the
managed policies recorded in state, deletes the local-irsa owned IAM Role, and
removes the binding from state.

If the ServiceAccount is already missing, annotation removal is skipped and AWS
cleanup continues.

`unbind` does not delete a ServiceAccount, even when that ServiceAccount was
created by `bind --create-service-account`.

## Next Step

Run [`down`](down.md) when you are done with the cluster-level resources. If
you used the demo policy, run [`demo delete-policy`](demo.md) after the policy
is detached from all roles.

## Main Failure Conditions

- The binding is not found in local-irsa state.
- The IAM Role is not owned by the current local-irsa cluster.
- AWS credentials or Kubernetes context cannot be used.
