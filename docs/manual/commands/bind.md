# `bind`

## Purpose

`bind` creates or updates one IAM Role for one Kubernetes ServiceAccount,
attaches existing managed policy ARNs, and writes the IRSA annotations to the
ServiceAccount.

## Prerequisites

Run `install` first. Prepare at least one AWS managed policy ARN or customer
managed policy ARN. For a small test, create the policy with
[`demo create-policy`](demo.md).

## Command

```text
go tool local-irsa bind --name NAME --namespace NS --service-account SA --role-name ROLE --policy-arn ARN... [--context CONTEXT] [--profile PROFILE] [--create-service-account]
```

## Required Flags

- `--name`
- `--namespace`
- `--service-account`
- `--role-name`
- one or more `--policy-arn`

Repeat `--policy-arn` to attach more than one managed policy to the same IAM
Role.

## Resources

`bind` creates or updates an IAM Role owned by local-irsa, attaches the policy
ARNs, and annotates the ServiceAccount with the role ARN and IRSA settings.

The ServiceAccount must already exist unless you pass
`--create-service-account`. When `--create-service-account` is set, `bind`
creates the ServiceAccount in the target namespace if it is missing.

One ServiceAccount has one role ARN. To add more AWS permissions, attach more
managed policy ARNs to the same role.

## Next Step

Run [`demo run`](demo.md) for the demo ServiceAccount, or run
[`doctor`](doctor.md) to check the binding.

## Main Failure Conditions

- The local state does not exist.
- No `--policy-arn` value is supplied.
- The ServiceAccount does not exist and `--create-service-account` is not set.
- The IAM Role is owned by another local-irsa cluster or by another owner.
