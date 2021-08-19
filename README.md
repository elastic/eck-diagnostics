# eck-diagnostics
Diagnostic tooling for ECK installations

[![Go](https://github.com/elastic/eck-diagnostics/actions/workflows/go.yml/badge.svg?branch=main)](https://github.com/elastic/eck-diagnostics/actions/workflows/go.yml)

## Installation

Go to the [releases](https://github.com/elastic/eck-diagnostics/releases) page and download the version matching your architecture. Unpack the gzip'ed tar archive and put the binary inside the archive somewhere in your PATH.


## Running

Just execute the binary. By default it will run diagnostics for the `elastic-system` namespace, where the ECK operator typically resides, and the `default` namespace.

To run diagnostics, for example, for namespaces `a` and `b` instead:
```shell
eck-diagnostics -r a -r b
```

A full list of available options is reproduced here and is also printed when calling the `eck-diagnostics` binary with the `--help` or `-h` flag:

```
Usage:
  eck-diagnostics [flags]

Flags:
      --eck-version string                 ECK version in use, will try to autodetect if not specified
  -h, --help                               help for eck-diagnostics
      --kubeconfig string                  optional path to kube config, defaults to $HOME/.kube/config
  -o, --operator-namespaces stringArray    Namespace(s) in which operator(s) are running (default [elastic-system])
      --output-directory string            Path where to output diagnostic results
  -r, --resources-namespaces stringArray   Namespace(s) in which resources are managed (default [default])
      --verbose                            Verbose mode

```
