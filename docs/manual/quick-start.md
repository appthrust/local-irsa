# Quick Start

This flow creates a local IRSA setup and verifies it with an AWS CLI Pod. The
goal is that `aws sts get-caller-identity` succeeds inside the Pod and uses the
IAM Role made by `local-irsa`.

Create local-irsa state and print the `kind` OIDC snippet.

```text
go tool local-irsa init --name <name> --region <region> --profile <profile>
```

Merge the printed snippet into your own `kind` config. Then create the cluster
yourself.

```text
kind create cluster --config <your-kind-config>
```

Install the S3 issuer and IAM OIDC Provider. This also installs the webhook
unless you pass `--skip-webhook`.

```text
go tool local-irsa install --name <name> --profile <profile>
```

Create the small demo customer managed policy.

```text
go tool local-irsa demo create-policy --name <name> --profile <profile>
```

Use the printed policy ARN to bind the demo ServiceAccount.

```text
go tool local-irsa bind \
  --name <name> \
  --namespace default \
  --service-account local-irsa-demo \
  --role-name local-irsa-<safeName(name)>-demo \
  --policy-arn <policyARN> \
  --create-service-account \
  --profile <profile>
```

Run the AWS CLI Pod.

```text
go tool local-irsa demo run --name <name>
```

The command succeeds when the Pod can read `AWS_ROLE_ARN`, read the projected
web identity token file, call `aws sts get-caller-identity`, and confirm the
IAM Role.

Clean up the demo binding, demo policy, and cluster-level local-irsa resources.

```text
go tool local-irsa unbind --name <name> --namespace default --service-account local-irsa-demo --profile <profile>
go tool local-irsa demo delete-policy --name <name> --profile <profile>
go tool local-irsa down --name <name> --delete-bucket --yes --profile <profile>
```

Command details:

- [`init`](commands/init.md)
- [Create a kind Cluster](kind.md)
- [`install`](commands/install.md)
- [Demo](commands/demo.md)
- [`bind`](commands/bind.md)
- [`unbind`](commands/unbind.md)
- [`down`](commands/down.md)
