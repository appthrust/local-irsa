# Overview

`local-irsa` is a CLI for trying AWS IRSA-style WebIdentity authentication on
a local `kind` cluster.

It prepares the public OIDC issuer data on S3, an IAM OIDC Provider, an IAM
Role for a Kubernetes ServiceAccount, and the IRSA annotations on that
ServiceAccount.

`local-irsa` is for development environments. It is not a complete production
operations tool, and it does not support issuer types other than S3.

`local-irsa` does not:

- create or delete `kind` clusters;
- install cert-manager for you;
- create, store, or copy the private signing key for ServiceAccount tokens;
- create or update policies, except for the small demo policy made by
  `demo create-policy`.

The ServiceAccount token private signing key stays inside the `kind` control
plane, where `kind` and kubeadm manage it. `local-irsa` reads only the public
JWKS from the Kubernetes API and publishes that public key data to the S3
issuer.

Common next pages:

- [IRSA Background](irsa-background.md)
- [Architecture](architecture.md)
- [Installation](installation.md)
- [Quick Start](quick-start.md)
- [State and AWS Settings](state-and-aws.md)
- [Troubleshooting](troubleshooting.md)
