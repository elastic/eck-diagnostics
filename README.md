# eck-diagnostics
Diagnostic tooling for ECK installations
```
Usage:
  eck-diagnostics [flags]

Flags:
      --eck-version string                 ECK version in use, will try to autodetect if not specified
  -h, --help                               help for eck-diagnostics
      --kubeconfig string                  optional path to kube config, defaults to $HOME/.kube/config
  -o, --operator-namespaces stringArray    Namespace(s) in which operator(s) are running (default [elastic-system])
      --output-directory string            Path where to output dump files
  -r, --resources-namespaces stringArray   Namespace(s) in which resources are managed (default [default])
      --verbose                            Verbose mode

```