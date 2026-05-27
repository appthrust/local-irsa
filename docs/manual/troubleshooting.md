# Troubleshooting

## Region Cannot Be Resolved

Cause: no region was passed and the AWS SDK cannot resolve one from the
environment or shared config profile.

Fix:

```text
go tool local-irsa init --name <name> --region <region> --profile <profile>
```

## OIDC Checks Fail During `install` or `doctor`

Cause: the cluster was created without the `kind` OIDC snippet, or the current
cluster does not match local-irsa state.

Check that the snippet from `kind-irsa-snippet.yaml` is merged into your
`kind` config, then recreate the cluster if needed.

```text
kind create cluster --config <your-kind-config>
go tool local-irsa install --name <name> --profile <profile>
```

## `doctor` Fails After Recreating the Cluster

Cause: recreating the cluster can change the ServiceAccount signing key and
JWKS.

Fix: publish the current cluster JWKS again.

```text
go tool local-irsa install --name <name> --profile <profile>
```

## cert-manager CRD Is Missing

Cause: webhook installation is enabled, but cert-manager is not installed in
the cluster.

Fix: install cert-manager before `go tool local-irsa install`, or skip webhook
installation.

```text
go tool local-irsa install --name <name> --skip-webhook --profile <profile>
```

## Webhook Readiness or Mutation Check Fails

Cause: the webhook Pod is not ready, cannot read ServiceAccounts, or cannot
mutate the dry-run test Pod.

Check the webhook Pod and logs.

```text
kubectl -n local-irsa-system get pods
kubectl -n local-irsa-system logs deploy/pod-identity-webhook
```

If `install` printed a reason, message, or admission response, check that
message first.

## S3 Public Read Setup Fails

Cause: S3 Block Public Access at the AWS account or bucket level can block the
public read policy for issuer objects.

Fix: check S3 Block Public Access for the target account and bucket.

## S3 Bucket Owner Is Different

Cause: the bucket name is owned by another AWS account.

Fix: run `init` without `--bucket`, or use a bucket name owned by your account.

```text
go tool local-irsa init --name <name> --region <region> --profile <profile>
```

## `bind` Cannot Find the ServiceAccount

Cause: the ServiceAccount does not exist and `--create-service-account` was
not set.

Fix: create it yourself, or let `bind` create it.

```text
go tool local-irsa bind --name <name> --namespace <namespace> --service-account <serviceAccount> --role-name <roleName> --policy-arn <policyARN> --create-service-account --profile <profile>
```

## `unbind` Cannot Find the Binding

Cause: `unbind` only removes bindings saved in local-irsa state.

Fix: check that `--namespace` and `--service-account` match the values used by
`bind`.

## `demo run` Cannot Find the ServiceAccount or Annotation

Cause: `bind` has not been run for the ServiceAccount, or different
ServiceAccount values were used.

Fix:

```text
go tool local-irsa bind --name <name> --namespace default --service-account local-irsa-demo --role-name local-irsa-<safeName(name)>-demo --policy-arn <policyARN> --create-service-account --profile <profile>
go tool local-irsa demo run --name <name>
```

## `demo run` Cannot Pull the Image

Cause: the cluster cannot pull the AWS CLI image. `demo run` reports the image,
reason, and message instead of waiting for the full timeout.

Fix: check network access from the cluster to the image registry.

## `demo delete-policy` Says the Policy Is Attached

Cause: an IAM Role still has the demo policy attached.

Fix: remove the binding or run cluster cleanup first.

```text
go tool local-irsa unbind --name <name> --namespace default --service-account local-irsa-demo --profile <profile>
go tool local-irsa demo delete-policy --name <name> --profile <profile>
```

## Ownership Tag Warning or Error

Cause: the AWS resource is not owned by the current local-irsa cluster.

Fix: check these tags on the resource:

- `local-irsa.appthrust.io/managed-by`;
- `local-irsa.appthrust.io/cluster`.
