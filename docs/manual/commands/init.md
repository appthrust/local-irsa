# `init`

## Purpose

`init` chooses local-irsa settings for one cluster and prints a `kind` OIDC
snippet. It does not create or update S3 buckets, S3 objects, IAM OIDC
Providers, IAM Roles, or ServiceAccount annotations.

## Prerequisites

You need AWS credentials and a region. The region can come from `--region`, an
environment variable, or the selected AWS profile.

## Command

```text
go tool local-irsa init --name NAME [--region REGION] [--bucket BUCKET] [--profile PROFILE]
```

## Required Flags

- `--name`

## Resources

`init` writes local state files only:

- `state.json`;
- `kind-irsa-snippet.yaml`.

When `--bucket` is not set, the bucket name uses this form:

```text
local-irsa-<accountID>-<region>-<safeName(name)>-<issuerID>
```

When `--bucket` is set, that bucket name is used as the issuer bucket. The
bucket must be owned by your AWS account.

The issuer URL uses this form:

```text
https://<bucket>.s3.<region>.amazonaws.com
```

## Next Step

Merge the printed snippet into your `kind` config, then create the cluster.
See [Create a kind Cluster](../kind.md).

## Main Failure Conditions

- AWS credentials cannot be resolved.
- AWS region cannot be resolved.
- Existing state has a different account ID, region, bucket, or issuer URL.
- A specified bucket is not usable by the current AWS account.
