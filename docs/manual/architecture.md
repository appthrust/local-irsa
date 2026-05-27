# Architecture

`local-irsa` connects a local Kubernetes cluster to AWS STS by publishing the
cluster's public ServiceAccount signing keys as an OIDC issuer and then
creating IAM resources that trust that issuer. The Pod can then call AWS
resources allowed by the IAM Role policies.

```mermaid
flowchart LR
  APIServer["kind API server"]
  State["local-irsa state"]
  S3["S3 OIDC issuer"]
  IAMOIDC["IAM OIDC Provider"]
  Role["IAM Role"]
  SA["ServiceAccount"]
  Webhook["Pod Identity Webhook"]
  Pod["Pod"]
  STS["AWS STS"]
  AWSResource["Target AWS resource"]

  State -->|issuer URL and bucket| S3
  APIServer -->|public JWKS| S3
  S3 -->|issuer URL and JWKS URI| IAMOIDC
  IAMOIDC -->|trusted identity provider| Role
  Role -->|role ARN annotation| SA
  SA --> Webhook
  Webhook -->|token volume and env vars| Pod
  Pod -->|AssumeRoleWithWebIdentity| STS
  STS -->|validates token issuer and claims| Role
  Role -->|policy allows actions| AWSResource
  Pod -->|AWS API call with temporary credentials| AWSResource
```

## `init`

`init` creates local state and the `kind` OIDC snippet. The state records the
AWS account ID, region, bucket name, issuer URL, and related values that later
commands need.

When `--bucket` is not set, `init` adds a random `issuerID` to the default
bucket name. This makes the issuer bucket name harder to guess.

`init` does not create AWS resources or Kubernetes resources. After `init`,
merge the printed snippet into your `kind` config. See [`init`](commands/init.md)
and [Create a kind Cluster](kind.md).

## kind Cluster Creation

You create the `kind` cluster yourself after merging the snippet. The snippet
sets the kube-apiserver issuer URL and JWKS URL:

```text
service-account-issuer: "<issuerURL>"
service-account-jwks-uri: "<issuerURL>/keys.json"
```

`local-irsa` does not set the ServiceAccount private signing key path. The
private signing key stays inside the `kind` control plane. `local-irsa` reads
only the public JWKS from the Kubernetes API.

## `install`

`install` reads local state and the current cluster OIDC data. It publishes two
public S3 objects:

```text
/.well-known/openid-configuration
/keys.json
```

The discovery document points AWS to the issuer URL and the JWKS URI. The JWKS
contains public keys only. These objects must be public read so AWS can verify
web identity tokens.

`install` also creates or checks the IAM OIDC Provider for the S3 issuer URL.
IAM trusts that provider because it represents the issuer URL in the token.
See [`install`](commands/install.md).

## `bind`

`bind` creates or updates an IAM Role and writes the role ARN to one
ServiceAccount. The trust policy restricts the role to the expected token
claims:

- `aud` must be `sts.amazonaws.com`;
- `sub` must match `system:serviceaccount:<namespace>:<serviceAccount>`.

These conditions restrict the IAM Role to one Kubernetes ServiceAccount. To
grant more AWS permissions to that ServiceAccount, attach more managed policy
ARNs to the same role. See [`bind`](commands/bind.md).

## Pod Runtime Authentication

When a Pod uses a bound ServiceAccount, the Pod Identity Webhook injects the
web identity token volume and AWS environment variables. The application or
AWS CLI in the Pod calls AWS STS with `AssumeRoleWithWebIdentity`.

AWS STS checks the token issuer against the IAM OIDC Provider, reads the public
keys from the S3 JWKS object, validates the token, checks the role trust policy
conditions, and returns temporary credentials for the IAM Role. The Pod then
uses those temporary credentials to call the target AWS resources allowed by
the attached IAM policies.

Use [Demo](commands/demo.md) to run a small AWS CLI Pod that verifies this
path.

## Command and Runtime Sequence

This sequence shows both the setup commands and the runtime AWS credential
exchange.

```mermaid
sequenceDiagram
  actor User
  participant CLI as local-irsa
  participant Kind as kind cluster
  participant API as Kubernetes API server
  participant S3 as S3 OIDC issuer
  participant IAM as AWS IAM
  participant Webhook as Pod Identity Webhook
  participant Pod
  participant STS as AWS STS
  participant AWSResource as Target AWS resource

  User->>CLI: go tool local-irsa init
  CLI-->>User: state.json and kind snippet
  User->>Kind: merge snippet and kind create cluster
  Kind->>API: start API server with issuer and JWKS URL
  User->>CLI: go tool local-irsa install
  CLI->>API: read OIDC discovery and JWKS
  CLI->>S3: publish discovery document and keys.json
  CLI->>IAM: create or verify IAM OIDC Provider
  User->>CLI: go tool local-irsa demo create-policy or prepare policy
  User->>CLI: go tool local-irsa bind
  CLI->>IAM: create IAM Role and attach policy
  CLI->>API: annotate ServiceAccount with role ARN
  User->>API: create Pod or go tool local-irsa demo run
  API->>Webhook: admission review
  Webhook-->>API: inject token volume and AWS env vars
  API->>Pod: start Pod with ServiceAccount token projection
  Pod->>STS: AssumeRoleWithWebIdentity
  STS-->>Pod: temporary credentials for IAM Role
  Pod->>AWSResource: AWS API call allowed by IAM policy
```

## Safety Model

`local-irsa` uses a few safeguards around resources that can affect account
security:

- The default bucket name includes a random `issuerID` when `--bucket` is not
  set.
- S3 operations check the expected bucket owner.
- Managed AWS resources use local-irsa ownership tags.
- Existing resources whose ownership tags do not match are not changed.
- `down --delete-bucket` deletes the S3 bucket only after the IAM OIDC
  Provider is deleted or confirmed absent.

The last rule avoids releasing an issuer bucket name while an IAM OIDC
Provider in the account still trusts that issuer URL. See [`down`](commands/down.md)
for cleanup details.
