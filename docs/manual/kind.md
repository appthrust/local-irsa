# Create a kind Cluster

Run `init` before you create the cluster. `init` prints a `kind` config
snippet and writes the same snippet to the state directory.

Merge the snippet into your own `kind` config, then create the cluster
yourself.

```text
kind create cluster --config <your-kind-config>
```

The generated snippet passes the issuer URL and JWKS URL to the kube-apiserver.

```yaml
nodes:
  - role: control-plane
    kubeadmConfigPatches:
      - |
        kind: ClusterConfiguration
        apiServer:
          extraArgs:
            service-account-issuer: "<issuerURL>"
            service-account-jwks-uri: "<issuerURL>/keys.json"
```

The snippet does not set `service-account-signing-key-file` or
`service-account-key-file`.

If you create the cluster without the snippet, `install` and `doctor` fail
during OIDC checks because the cluster issuer data does not match local-irsa
state.

If you delete and recreate the cluster, the ServiceAccount signing key and
JWKS can change. Run `go tool local-irsa install --name <name>` again after recreating
the cluster, even when the issuer URL is the same.
