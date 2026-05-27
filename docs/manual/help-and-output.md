# Help and Output

Show the root help to see the available subcommands.

```text
go tool local-irsa --help
```

Show command help to see flags, required values, and examples.

```text
go tool local-irsa init --help
go tool local-irsa install --help
go tool local-irsa bind --help
go tool local-irsa unbind --help
go tool local-irsa doctor --help
go tool local-irsa down --help
go tool local-irsa demo create-policy --help
go tool local-irsa demo run --help
go tool local-irsa demo delete-policy --help
```

The `init`, `install`, `bind`, `unbind`, `doctor`, `down`, and `demo`
commands print progress steps while they work. Progress lines, warnings, and
errors go to standard error. Final success results and next steps go to
standard output.

Use `--quiet` before the subcommand to hide successful progress lines.

```text
go tool local-irsa --quiet install --name <name>
```

Use `--verbose` before the subcommand to show the main AWS, S3, IAM, and
kubectl operation targets.

```text
go tool local-irsa --verbose install --name <name>
```

`--quiet` and `--verbose` cannot be used together. `--verbose` does not print
secrets or tokens.
