# `install`

## Purpose

`install` reads local-irsa state and the current Kubernetes cluster, publishes
the S3 issuer documents, and creates or checks the IAM OIDC Provider. By
default, it also installs `amazon-eks-pod-identity-webhook`.

## Prerequisites

Run `init`, merge the snippet into your `kind` config, and create the cluster
before running `install`.

The selected AWS credentials need IAM, S3, and STS access. If you do not pass
`--skip-webhook`, the cluster must already have cert-manager.

## Command

```text
go tool local-irsa install --name NAME [--context CONTEXT] [--profile PROFILE] [--skip-webhook]
```

## Required Flags

- `--name`

## Resources

`install` checks the cluster OIDC settings, reads the cluster JWKS, publishes
these S3 objects, and creates or checks the IAM OIDC Provider:

```text
/.well-known/openid-configuration
/keys.json
```

The two S3 objects are public read. Other bucket objects are not made public.

If the S3 bucket or IAM OIDC Provider already exists, `install` checks
ownership before changing it. A resource owned by another account or another
local-irsa cluster is not changed.

## Webhook

By default, `install` applies `amazon-eks-pod-identity-webhook`:

```text
public.ecr.aws/eks/amazon-eks-pod-identity-webhook:v0.6.15
```

The webhook runs as `pod-identity-webhook` in the `local-irsa-system`
namespace. The MutatingWebhookConfiguration uses
`admissionReviewVersions: v1beta1`.

Before changing AWS resources, `install` checks that the cert-manager
`certificates.cert-manager.io` CRD exists. After applying the webhook,
`install` waits up to 120 seconds for the Deployment to become Available. It
then checks that the webhook can read ServiceAccounts and can mutate a test Pod
with IRSA environment variables and the projected token volume. This mutation
check uses server-side dry-run, so no image is pulled.

Pass `--skip-webhook` to skip cert-manager checks, webhook installation,
webhook readiness, and the mutation check.

## Next Step

Run [`bind`](bind.md) for your ServiceAccount, or run
[`demo create-policy`](demo.md) first for a small end-to-end check.

## Main Failure Conditions

- The cluster was created without the `kind` OIDC snippet.
- The cluster JWKS cannot be read.
- cert-manager is missing when webhook installation is enabled.
- The webhook becomes unavailable or fails the mutation check.
- The S3 bucket, IAM OIDC Provider, or related resource is not owned by the
  current local-irsa cluster.
