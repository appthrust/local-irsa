# Contributing

Thank you for improving `local-irsa`.

## Development Environment

Use these tools for local development:

- Go 1.26 or later;
- devbox;
- Taskfile;
- kind;
- kubectl;
- a Docker-compatible container runtime.

Install Go module dependencies.

```text
task install
```

Run the standard checks before opening a pull request.

```text
task check
```

`task check` runs Go tests and vet checks. It does not require AWS
credentials, a kind cluster, Docker daemon access, or external AWS resources.

## Local kind Cluster

Manual kind checks use a local `kind.yaml` file. Start from the tracked sample.

```text
cp kind.dist.yaml kind.yaml
```

When you need local-irsa OIDC settings, run the development build and merge
the printed kind config snippet into `kind.yaml`.

Start or reuse the development cluster.

```text
task up
```

Stop the development cluster.

```text
task down
```

`task up` installs cert-manager `v1.20.2` into the development kind cluster.
It does not run local-irsa commands such as `init`, `install`, `bind`,
`doctor`, or `down`.
`task down` deletes the kind cluster and does not delete AWS resources.

The development cluster name is read from `LOCAL_IRSA_DEV_CLUSTER`. If it is
not set, `local-irsa-dev` is used. Keep this value in sync with the cluster
name in `kind.yaml`.

## Optional AWS End-to-End Checks

End-to-end checks that touch AWS are optional. Use your own AWS account,
credentials, and permissions. Do not publish personal AWS account IDs, profile
names, bucket names, issuer URLs, credentials, or tokens in issues, pull
requests, logs, or documentation.

Use placeholders such as `<profile>`, `<accountID>`, `<bucket>`, and
`<issuerURL>` in examples.

Confirm your AWS account before a check.

```text
aws sts get-caller-identity --profile <profile>
```

Then prepare a local kind config, start the development cluster, and run the
local-irsa flow with your own names and profile.

```text
cp kind.dist.yaml kind.yaml
go run ./cmd/local-irsa init --name <name> --region <region> --profile <profile>
# Merge the printed kind config snippet into kind.yaml.
task up
go run ./cmd/local-irsa install --name <name> --profile <profile>
go run ./cmd/local-irsa demo create-policy --name <name> --profile <profile>
go run ./cmd/local-irsa bind --name <name> --namespace default --service-account local-irsa-demo --role-name local-irsa-<safeName(name)>-demo --policy-arn <policyARN> --create-service-account --profile <profile>
go run ./cmd/local-irsa demo run --name <name>
go run ./cmd/local-irsa doctor --name <name> --namespace default --service-account local-irsa-demo --profile <profile>
go run ./cmd/local-irsa down --name <name> --delete-bucket --yes --profile <profile>
go run ./cmd/local-irsa demo delete-policy --name <name> --profile <profile>
```

## Documentation and Design

Pull requests that change CLI behavior should update the user manual or the
design document.

Update documentation when you change:

- CLI flags;
- command output;
- AWS or Kubernetes resources;
- cleanup behavior;
- security-sensitive behavior.

## Security Reports

Do not report security issues in public issues. This includes problems related
to AWS credentials, ServiceAccount tokens, OIDC issuer takeover, IAM trust
policies, and S3 bucket policies.

This repository must publish `SECURITY.md` with a private reporting contact
before OSS publication.
