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

```shell
Usage:
  eck-diagnostics [flags]

Flags:
      --diagnostic-image string              Diagnostic image to be used for stack diagnostics, see run-stack-diagnostics (default "docker.elastic.co/eck-dev/support-diagnostics:9.3.1")
      --eck-version string                   ECK version in use, will try to autodetect if not specified
  -f, --filters strings                      Comma-separated list of filters in format "type=name". Example: elasticsearch=my-cluster (Supported types [agent apm beat elasticsearch enterprisesearch kibana maps logstash])
  -h, --help                                 help for eck-diagnostics
      --kubeconfig string                    optional path to kube config, defaults to $HOME/.kube/config
  -l, --log-selectors stringArray            Label selectors to restrict the logs to be collected. Can be specified more than once. Example: -l 'elasticsearch.k8s.elastic.co/node-master=true,elasticsearch.k8s.elastic.co/node-data!=true' -l common.k8s.elastic.co/type=kibana.
  -o, --operator-namespaces strings          Comma-separated list of namespace(s) in which operator(s) are running (default [elastic-system])
      --output-directory string              Path where to output diagnostic results
  -n, --output-name string                   Name of the output diagnostics file (default "eck-diagnostics-2024-06-25T09-28-37.zip")
  -r, --resources-namespaces strings         Comma-separated list of namespace(s) in which resources are managed
      --run-agent-diagnostics                Run diagnostics on deployed Elastic Agents. Warning: credentials will not be redacted and appear as plain text in the archive
      --run-stack-diagnostics                Run diagnostics on deployed Elasticsearch clusters and Kibana instances, requires deploying diagnostic Pods into the cluster (default true)
      --stack-diagnostics-timeout duration   Maximum time to wait for Elaticsearch and Kibana diagnostics to complete (default 5m0s)
      --verbose                              Verbose mode
  -v, --version                              version for eck-diagnostics
```

## Information collected by eck-diagnostics

The eck-diagnostics retrieves Kubernetes API server resources and log files and, unless disabled, it runs Elastic [support-diagnostics](https://github.com/elastic/support-diagnostics) on Elasticsearch and Kibana instances installed in the namespaces indicated by the `-r, --resources-namespaces` flag. In addition it can optionally run the [diagnostic subcommand](https://www.elastic.co/guide/en/fleet/current/elastic-agent-cmd-options.html#elastic-agent-diagnostics-command) on all running Elastic Agents. Please note that credentials are not redacted in the Elastic Agent diagnostics and may appear in plain text inside the diagnostics archive. Elastic Agent diagnostics are therefore disabled by default.

The following Kubernetes resources are retrieved from the cluster being diagnosed:

### Global resources

* Kubernetes server version
* Kubernetes nodes
* PodSecurityPolicy
* ClusterRole
* ClusterRoleBindings
* StorageClass

### In all operator and workload resource namespaces

* StatefulSet
* Pod
* Service
* ConfigMap
* Event
* NetworkPolicy
* ControllerRevision
* Secret (metadata only)
* ServiceAccount

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
* AutoOpsAgentPolicy
* Beat
* ElasticMapsServer
* Elasticsearch
* EnterpriseSearch
* Kibana
* PackageRegistry

### Logs

In the operator namespaces (`-o, --operator-namespaces`) the operator's logs are collected, while in the workload resource namespaces all logs from Pods managed by ECK are collected.

## Filtering collected resources

The resources in the specified namespaces that are collected by eck-diagnostics can be filtered with the `-f, --filters` and `-l, --log-selectors` flags.

### Usage Examples

The following example will run the diagnostics for Elastic resources in namespace `a`, and will only return resources associated with either an Elasticsearch cluster named `mycluster` or a Kibana instance named `my-kb`.

```shell
eck-diagnostics -r a -f "elasticsearch=mycluster" -f "kibana=my-kb"
```

To restrict the amount of logs collected by eck-diagnostics use the `-l, --log-selectors` flag (repeatedly). For example to only collect the logs for Kibana and the Elasticsearch master nodes that are not data nodes:


```shell
eck-diagnostics -r a -f "elasticsearch=mycluster" -f "kibana=my-kb" -l 'elasticsearch.k8s.elastic.co/node-master=true,elasticsearch.k8s.elastic.co/node-data!=true' -l common.k8s.elastic.co/type=kibana
```

The log selector flags `-l` can also be used without the filter flags `-f` and [set-based Kubernetes requirement syntax](https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#set-based-requirement) is also supported:

```shell
eck-diagnostics -r a  -l 'apps.kubernetes.io/pod-index notin(0,1)'
```

### Filtered resources

Only a certain number of Kubernetes resources support filtering when filtering by an Elastic custom resource.  Along with the named Elastic custom resource type, the following resources will be returned that are associated:

* ConfigMap
* ControllerRevision
* Deployment
* DaemonSet
* Endpoint
* Pod
* PersistentVolumeClaim
* Replicaset
* Service
* StatefulSet

The following resources are returned unfiltered:

* Event
* NetworkPolicy
* PersistentVolume
* ServiceAccount
* Secret (metadata only)

### Using custom ECK installation methods

If you are using your own installation manifests or Helm chart make sure the metadata on the Pods running the operator matches the metadata used by Elastic's official installation manifests. Specifically ensure that you have a `control-plane: elastic-operator` label on the ECK operator StatefulSet or Deployment and its Pods. This ensures that eck-diagnostics can successfully extract logs and version information from those Pods.
