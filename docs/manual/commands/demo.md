# Demo

## Purpose

`demo` commands help you run a small end-to-end check. They do not replace the
normal `init`, `install`, and `bind` commands.

The demo flow creates a small customer managed policy, binds a demo
ServiceAccount, runs an AWS CLI Pod, and then deletes the demo policy after the
role attachment is removed.

## Prerequisites

Run `init`, create the `kind` cluster, and run `install` first.

## Create the Demo Policy

```text
go tool local-irsa demo create-policy --name NAME [--profile PROFILE]
```

`demo create-policy` creates or checks the demo customer managed policy. It
does not run `bind`. On success, it prints the policy ARN and a suggested
`bind` command.

## Bind the Demo ServiceAccount

Use the printed policy ARN with `bind`.

```text
go tool local-irsa bind --name NAME --namespace default --service-account local-irsa-demo --role-name local-irsa-<safeName(name)>-demo --policy-arn <policyARN> --create-service-account
```

The demo defaults are:

- namespace: `default`;
- ServiceAccount: `local-irsa-demo`;
- role name: `local-irsa-<safeName(name)>-demo`;
- policy name: `local-irsa-<safeName(name)>-demo`.

## Run the AWS CLI Pod

```text
go tool local-irsa demo run --name NAME [--namespace NS] [--service-account SA] [--context CONTEXT]
```

`demo run` starts a temporary AWS CLI Pod with the bound ServiceAccount. It
checks `AWS_ROLE_ARN`, `AWS_WEB_IDENTITY_TOKEN_FILE`,
`aws sts get-caller-identity`, and `aws iam get-role`. It does not print the
ServiceAccount token.

Before creating the Pod, `demo run` prints the equivalent `kubectl run`
command. The Pod uses `--attach=true`, `--rm=true`, and
`--pod-running-timeout=120s`.

`demo run` does not run `install`, `bind`, or `doctor`. It fails if the
ServiceAccount is missing or has no IRSA annotation.

## Delete the Demo Policy

Detach the policy first by running `unbind` or `down`.

```text
go tool local-irsa unbind --name NAME --namespace default --service-account local-irsa-demo
go tool local-irsa demo delete-policy --name NAME [--profile PROFILE]
```

`demo delete-policy` deletes only the demo customer managed policy. It does
not delete IAM Roles, ServiceAccount annotations, the S3 issuer, or the IAM
OIDC Provider.

## Main Failure Conditions

- The state does not exist.
- The demo policy exists but is not owned by the current local-irsa cluster.
- The demo ServiceAccount is not bound before `demo run`.
- The AWS CLI image cannot be pulled.
- The demo policy is still attached to an IAM Role when
  `demo delete-policy` runs.
