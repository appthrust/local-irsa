# Development Tasks

## Taskfile Commands

`Taskfile.yaml` provides development helper tasks. These tasks do not change
the CLI contract. The CLI still does not create or delete `kind` clusters.

Install Go module dependencies:

```text
task install
```

`task install` does not check whether external commands are installed.

Run standard Go checks:

```text
task check
```

`task check` runs at least:

```text
go test ./...
go vet ./...
```

Start or reuse the development `kind` cluster:

```text
task up
```

`task up` uses the developer's `kind.yaml`. It prepares cluster prerequisites
needed by the default `local-irsa install` flow. It does not download Go
modules, run tests, run static checks, run `local-irsa`, merge the OIDC
snippet, check AWS profiles, or create AWS resources.

`task up` does these steps:

1. Check that `kind.yaml` exists.
2. Fail with setup guidance when `kind.yaml` is missing.
3. Create or reuse the `kind` cluster with `kind create cluster --config kind.yaml`.
4. Apply the cert-manager manifest.
5. Wait for the `certificates.cert-manager.io` CRD to become Established.
6. Wait for cert-manager Deployments to become Available.
7. Print the kind config, kind context, and cert-manager version.

The cert-manager version is fixed to `v1.20.2`. Do not use a `latest` tag or
an unpinned URL. The manifest URL has this form:

```text
https://github.com/cert-manager/cert-manager/releases/download/v1.20.2/cert-manager.yaml
```

The cert-manager readiness timeout is 120 seconds. When it times out,
`task up` prints a command for checking Pod status:

```text
kubectl -n cert-manager get pods
```

Stop the development `kind` cluster:

```text
task down
```

`task down` deletes the development `kind` cluster. It does not run
`local-irsa down` and does not delete AWS resources. Deleting the cluster also
removes cert-manager installed by `task up`.

## kind Config

The repository tracks `kind.dist.yaml` as a sample. It does not include AWS
account IDs, bucket names, issuer URLs, `service-account-issuer`, or
`service-account-jwks-uri`.

Create a local copy:

```text
cp kind.dist.yaml kind.yaml
```

`kind.yaml` is local to each developer and is not tracked by git.

When you need local-irsa OIDC settings, run `local-irsa init`, merge the
printed snippet into `kind.yaml`, and then run `task up`. `task up` does not
run `local-irsa init`, merge the snippet, or run `install`.

## Environment Variables

`task up` and `task down` read `LOCAL_IRSA_DEV_CLUSTER` as the development
cluster name. The default is `local-irsa-dev`.

```text
LOCAL_IRSA_DEV_CLUSTER=<kind cluster name>
```

When you override this value, keep it in sync with the cluster name in
`kind.yaml`.

AWS region, AWS profile, and bucket name are not required inputs for
`task up`.

Use `LOCAL_IRSA_STATE_ROOT` when you want test state in a temporary directory.

```text
LOCAL_IRSA_STATE_ROOT=/tmp/local-irsa-dev
```
