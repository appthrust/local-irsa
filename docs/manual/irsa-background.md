# IRSA Background

IRSA means IAM Roles for Service Accounts. It lets a Kubernetes Pod use an IAM
Role without storing long-lived AWS credentials in the Pod.

## ServiceAccounts and Tokens

A Kubernetes ServiceAccount is an identity inside a namespace. A Pod can run as
that ServiceAccount.

Kubernetes can project a short-lived ServiceAccount token into the Pod. The
token is a signed JWT. Important claims include:

- `sub`, which identifies the ServiceAccount as
  `system:serviceaccount:<namespace>:<serviceAccount>`;
- `aud`, which names the intended audience for the token.

For AWS WebIdentity authentication, `aud` is usually `sts.amazonaws.com`.

## OIDC Issuer and JWKS

An OIDC issuer is the HTTPS identity provider named in the token. It publishes
a discovery document and public signing keys.

The discovery document is served at:

```text
/.well-known/openid-configuration
```

The discovery document points to the JWKS location. JWKS means JSON Web Key
Set. It contains public keys that a verifier can use to check token
signatures.

With `local-irsa`, the issuer documents are published to S3:

```text
/.well-known/openid-configuration
/keys.json
```

These objects are public read so AWS can verify projected ServiceAccount
tokens. They do not contain the private signing key.

## IAM OIDC Provider and Role Trust

AWS IAM uses an IAM OIDC Provider to trust tokens from an issuer URL. An IAM
Role can then allow `sts:AssumeRoleWithWebIdentity` from that provider.

The role trust policy checks token claims. `local-irsa` restricts the role to
one ServiceAccount by using conditions for:

- `aud`, which must be `sts.amazonaws.com`;
- `sub`, which must match one Kubernetes ServiceAccount.

After AWS STS accepts the token, it returns temporary credentials for the IAM
Role. The Pod uses those temporary credentials to call the AWS resources
allowed by the role's attached policies.

## Why This Avoids Long-Lived Credentials

Without IRSA, a local test Pod might need static AWS access keys in a Secret or
environment variable. Those keys can leak and can live longer than the test.

With IRSA, the Pod receives a short-lived Kubernetes token and exchanges it
with AWS STS. The AWS credentials returned by STS are temporary and scoped by
the IAM Role policies.

## EKS IRSA and local-irsa

On EKS, AWS provides the cluster issuer and control-plane integration. Users
create an IAM OIDC Provider for the EKS issuer and bind IAM Roles to
Kubernetes ServiceAccounts.

With `local-irsa`, the Kubernetes cluster is local. `local-irsa` publishes the
local cluster issuer through S3, configures `kind` explicitly with the issuer
URL and JWKS URL, creates or checks the IAM OIDC Provider, and annotates the
ServiceAccount.

Next: [Architecture](architecture.md).
