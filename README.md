# eck-diagnostics
Diagnostic tooling for ECK installations

[![Go](https://github.com/elastic/eck-diagnostics/actions/workflows/go.yml/badge.svg?branch=main)](https://github.com/elastic/eck-diagnostics/actions/workflows/go.yml)

## Installation

Go to the [releases](https://github.com/elastic/eck-diagnostics/releases) page and download the version matching your architecture. Unpack the gzip'ed tar archive and put the binary included in the archive somewhere in your PATH.


## Running

Just execute the binary. On macOS versions with Gatekeeper enabled you have to explicitly allow the execution the first time round, as described in this [support article](https://support.apple.com/en-us/HT202491) (in the section "If you want to open an app that hasn’t been notarized or is from an unidentified developer"). 

To run diagnostics, you need to specify at least the workload resource namespace(s). The operator namespace is set by default to the `elastic-system` namespace, where the ECK operator typically resides.

For example, to run diagnostics for resource namespaces `a` and `b`:
```shell
eck-diagnostics -r a,b
```

A full list of available flags is reproduced here and is also printed when calling the `eck-diagnostics` binary with the `--help` or `-h` flag:

```
Usage:
  eck-diagnostics [flags]

Flags:
      --diagnostic-image string        Diagnostic image to be used for stack diagnostics, see run-stack-diagnostics (default "docker.elastic.co/eck-dev/support-diagnostics:8.1.4")
      --eck-version string             ECK version in use, will try to autodetect if not specified
  -h, --help                           help for eck-diagnostics
      --kubeconfig string              optional path to kube config, defaults to $HOME/.kube/config
  -o, --operator-namespaces strings    Comma-separated list of namespace(s) in which operator(s) are running (default [elastic-system])
      --output-directory string        Path where to output diagnostic results
  -r, --resources-namespaces strings   Comma-separated list of namespace(s) in which resources are managed
      --run-stack-diagnostics          Run diagnostics on deployed Elasticsearch clusters and Kibana instances, requires deploying diagnostic Pods into the cluster (default true)
      --verbose                        Verbose mode
```

## Information collected by eck-diagnostics

The eck-diagnostics retrieves Kubernetes API server resources and log files and, unless disabled, it runs Elastic [support-diagnostics](https://github.com/elastic/support-diagnostics) on Elasticsearch and Kibana instances installed in the namespaces indicated by the `-r, --resources-namespaces` flag.

The following Kubernetes resources are retrieved from the cluster being diagnosed:

### Global resources
* Kubernetes server version
* Kubernetes nodes
* PodSecurityPolicy
* ClusterRole

### In all operator and workload resource namespaces 
* StatefulSet
* Pod
* Service
* ConfigMap
* Event
* NetworkPolicy
* ControllerRevision
* Secret (metadata only)

### In the workload resources namespaces
In addition to the resources mentioned above, the following Kubernetes resources are retrieved:
* ReplicaSet
* Deployment
* DaemonSet
* PersistentVolume
* PersistentVolumeClaim
* Endpoint

The ECK related custom resources are included in those namespaces as well: 
* Agent
* ApmServer
* Beat
* ElasticMapsServer
* Elasticsearch
* EnterpriseSearch
* Kibana

### Logs
In the operator namespaces (`-o, --operator-namespaces`) all logs are collected, while in the workload resource namespaces only logs from Pods managed by ECK are collected.