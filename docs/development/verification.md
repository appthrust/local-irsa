# Verification

## AWS Verification

End-to-end checks that touch AWS need AWS credentials and IAM, S3, and STS
permissions. The AWS CLI is not required by the implementation, but it is
useful for checking the selected profile.

Use your own AWS account and shared config profile. Do not put real AWS
account IDs, profile names, bucket names, issuer URLs, credentials, or tokens
in public docs, issues, pull requests, or logs. Use placeholders such as
`<accountID>`, `<profile>`, `<bucket>`, and `<issuerURL>`.

Confirm the account before a check:

```text
aws sts get-caller-identity --profile <profile>
```

Check that the returned `Account` matches `<accountID>`.

You can test `init` without creating AWS resources:

```text
LOCAL_IRSA_STATE_ROOT=/tmp/local-irsa-dev \
  go run ./cmd/local-irsa init \
  --name <name> \
  --region <region> \
  --profile <profile>
```

`init` does not create or update S3 buckets, S3 objects, IAM OIDC Providers,
IAM Roles, or ServiceAccount annotations.

## Manual Verification

A full manual check uses this flow:

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
task down
```

`install`, `bind`, `doctor`, `down`, and the demo commands can change AWS or
Kubernetes resources. Use test names, buckets, IAM Roles, and ServiceAccounts.
Clean up local-irsa managed resources at the end.

When checking webhook behavior, the cluster must have cert-manager. `task up`
installs cert-manager in the development `kind` cluster. `local-irsa install`
does not install cert-manager. To check only the issuer and IAM OIDC Provider,
use:

```text
go run ./cmd/local-irsa install --name <name> --skip-webhook --profile <profile>
```

## Regular Checks

After code changes, run at least:

```text
go test ./...
go vet ./...
task --list
```

Run broader manual checks when you change CLI flags, command output, AWS or
Kubernetes resources, cleanup behavior, or security-sensitive behavior.
