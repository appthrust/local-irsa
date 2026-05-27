# State and AWS Settings

`local-irsa` stores cluster state under this directory by default:

```text
~/.local/share/local-irsa/clusters/<name>/state.json
```

Set `LOCAL_IRSA_STATE_ROOT` to use another state root.

After `init`, the state directory contains:

- `state.json`;
- `kind-irsa-snippet.yaml`.

After `install`, it also contains local copies of the issuer documents:

- `openid-configuration.json`;
- `keys.json`.

`local-irsa` does not create `sa.key` or `sa.pub` in the state directory. The
state file stores values needed by the commands. It does not store timestamp
metadata such as `initializedAt`. When `--bucket` is not set, `init` also
stores an `issuerID` so the issuer URL is hard to guess.

## AWS Region and Profile

When `--region` is set, `local-irsa` uses that region. When it is not set, the
AWS SDK resolves the region from the environment or shared config profile. The
command fails if no region can be resolved.

When `--profile` is set, `local-irsa` uses that shared config profile. When it
is not set, AWS credentials are resolved by the normal AWS SDK chain.

## Ownership Tags

AWS resources managed by `local-irsa` have ownership tags. Before updating or
deleting an existing resource, `local-irsa` checks these tags:

- `local-irsa.appthrust.io/managed-by`;
- `local-irsa.appthrust.io/cluster`.

If the tags do not match the current cluster state, `local-irsa` does not
change the resource.

## Public S3 Issuer Objects

`install` publishes these S3 objects:

```text
/.well-known/openid-configuration
/keys.json
```

The bucket policy allows public read only for these two objects. Other objects
in the bucket are not made public by `local-irsa`.
