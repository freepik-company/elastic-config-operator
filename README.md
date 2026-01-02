# Elastic Config Operator
### 🚀 Manage Your Elasticsearch/OpenSearch Config Like a Pro!

<img src="https://raw.githubusercontent.com/freepik-company/elastic-config-operator/master/docs/img/logo.png" alt="Elastic Config Operator Logo." width="150">

![GitHub Release](https://img.shields.io/github/v/release/freepik-company/elastic-config-operator)
![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/freepik-company/elastic-config-operator)
[![Go Report Card](https://goreportcard.com/badge/github.com/freepik-company/elastic-config-operator)](https://goreportcard.com/report/github.com/freepik-company/elastic-config-operator)
![GitHub License](https://img.shields.io/github/license/freepik-company/elastic-config-operator)

A Kubernetes operator to manage Elasticsearch and OpenSearch configuration (ILM/ISM policies, Index Templates, Snapshot Lifecycle Policies, Snapshot Repositories, and Cluster Settings) as Kubernetes Custom Resources.

## Overview

The Elastic Config Operator enables declarative management of Elasticsearch and OpenSearch configuration through Kubernetes Custom Resources. It provides automated lifecycle management for ILM/ISM policies, Index Templates, Snapshot configurations, and Cluster Settings.

### Key Features

- **Declarative Configuration**: Define Elasticsearch/OpenSearch resources as Kubernetes manifests
- **ECK Integration**: Automatic discovery of Elasticsearch endpoints and credentials from Elastic Cloud on Kubernetes (ECK)
- **External Cluster Support**: Connect to any Elasticsearch/OpenSearch cluster with manual configuration
- **Resource Tracking**: Automatic detection and cleanup of configuration drift
- **Status Reporting**: Detailed phase tracking (Pending, Syncing, Ready, Error) with timestamps
- **Connection Pooling**: Efficient reuse of HTTP connections across reconciliation cycles
- **Configurable Sync Intervals**: Per-resource control of reconciliation frequency
- **Dual Platform Support**: Compatible with both Elasticsearch and OpenSearch

## Supported Resources

| Custom Resource | Elasticsearch API | OpenSearch API | Notes |
|----------------|-------------------|----------------|-------|
| `ClusterSettings` | ✅ Cluster Settings | ✅ Cluster Settings | Fully compatible |
| `IndexLifecyclePolicy` | ✅ Index Lifecycle Management (ILM) | ❌ Not supported | Elasticsearch only |
| `IndexStateManagement` | ❌ Not supported | ✅ Index State Management (ISM) | OpenSearch only |
| `IndexTemplate` | ✅ Index Templates | ✅ Index Templates | Fully compatible |
| `SnapshotLifecyclePolicy` | ✅ Snapshot Lifecycle Management (SLM) | ✅ Snapshot Lifecycle Management (SLM) | Fully compatible |
| `SnapshotRepository` | ✅ Snapshot Repositories | ✅ Snapshot Repositories | Fully compatible |

## Deployment

The operator supports deployment via Kustomize or Helm, enabling GitOps workflows with tools like ArgoCD or FluxCD.

### Option 1: Using Kustomize

Reference the desired release version in your Kustomization manifest:

```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- https://github.com/freepik-company/elastic-config-operator/releases/download/v0.1.0/install.yaml
```

### Option 2: Using Helm

Add the Helm repository and install:

```bash
helm repo add elastic-config-operator https://freepik-company.github.io/elastic-config-operator/
helm repo update
helm install elastic-config-operator elastic-config-operator/elastic-config-operator \
  --namespace elastic-config-operator \
  --create-namespace
```

For production deployments with namespace-specific RBAC:

```bash
helm install elastic-config-operator elastic-config-operator/elastic-config-operator \
  --namespace elastic-config-operator \
  --create-namespace \
  --set rbac.secretAccess.enabled=true \
  --set rbac.secretAccess.namespaces="{elasticsearch-apps,opensearch-cluster}"
```

See the [Helm Chart documentation](./charts/elastic-config-operator/README.md) for detailed configuration options.

## Examples

Sample manifests for all resource types are available in the [config/samples](./config/samples) directory.

### Index Lifecycle Policy (Elasticsearch)

Define hot-warm-cold-delete lifecycle phases for Elasticsearch indices:

```yaml
apiVersion: elastic-config-operator.freepik.com/v1alpha1
kind: IndexLifecyclePolicy
metadata:
  name: my-ilm-policies
spec:
  syncInterval: "30s"  # Optional, defaults to 10s
  resourceSelector:
    name: elasticsearch  # ECK Elasticsearch resource name
    namespace: default   # Optional, defaults to CR namespace
  resources:
    hot-warm-cold:
      policy:
        phases:
          hot:
            min_age: "0ms"
            actions:
              rollover:
                max_age: "7d"
                max_size: "50gb"
          warm:
            min_age: "7d"
            actions:
              forcemerge:
                max_num_segments: 1
              shrink:
                number_of_shards: 1
          cold:
            min_age: "30d"
            actions:
              freeze: {}
          delete:
            min_age: "90d"
            actions:
              delete: {}
```

### Index State Management (OpenSearch)

Define ISM policies for OpenSearch clusters:

```yaml
apiVersion: elastic-config-operator.freepik.com/v1alpha1
kind: IndexStateManagement
metadata:
  name: my-ism-policies
spec:
  resourceSelector:
    name: opensearch
    clusterType: opensearch  # Required for OpenSearch
  resources:
    hot-warm-delete:
      description: "Hot-warm-delete policy for logs"
      default_state: "hot"
      states:
        - name: "hot"
          actions:
            - rollover:
                min_index_age: "1d"
                min_primary_shard_size: "50gb"
          transitions:
            - state_name: "warm"
              conditions:
                min_index_age: "7d"
        - name: "warm"
          actions:
            - replica_count:
                number_of_replicas: 1
            - force_merge:
                max_num_segments: 1
          transitions:
            - state_name: "delete"
              conditions:
                min_index_age: "30d"
        - name: "delete"
          actions:
            - delete: {}
```

### Index Template

Define composable index templates with mappings and settings:

```yaml
apiVersion: elastic-config-operator.freepik.com/v1alpha1
kind: IndexTemplate
metadata:
  name: my-index-templates
spec:
  resourceSelector:
    name: elasticsearch
  resources:
    logs-template:
      index_patterns:
        - "logs-*"
      priority: 100
      template:
        settings:
          number_of_shards: 1
          number_of_replicas: 1
          index:
            lifecycle:
              name: "30d-retention"
              rollover_alias: "logs"
        mappings:
          properties:
            "@timestamp":
              type: date
            message:
              type: text
            level:
              type: keyword
```

### Snapshot Repository

Configure snapshot storage backends (filesystem, S3, GCS, Azure):

```yaml
apiVersion: elastic-config-operator.freepik.com/v1alpha1
kind: SnapshotRepository
metadata:
  name: my-snapshot-repos
spec:
  resourceSelector:
    name: elasticsearch
  resources:
    my-fs-repository:
      type: fs
      settings:
        location: "/usr/share/elasticsearch/snapshots"
        compress: true
```

### Snapshot Lifecycle Policy

Automate snapshot scheduling and retention:

```yaml
apiVersion: elastic-config-operator.freepik.com/v1alpha1
kind: SnapshotLifecyclePolicy
metadata:
  name: my-slm-policies
spec:
  resourceSelector:
    name: elasticsearch
  resources:
    daily-snapshots:
      schedule: "0 0 1 * * ?"  # 1:00 AM daily (6-field cron format)
      name: "<daily-snap-{now/d}>"
      repository: my-fs-repository
      config:
        indices: ["*"]
        ignore_unavailable: false
        include_global_state: false
      retention:
        expire_after: "30d"
        min_count: 5
        max_count: 50
```

### Cluster Settings

Manage persistent and transient cluster-level settings:

```yaml
apiVersion: elastic-config-operator.freepik.com/v1alpha1
kind: ClusterSettings
metadata:
  name: my-cluster-settings
spec:
  resourceSelector:
    name: elasticsearch
  resources:
    # Persistent settings survive cluster restarts
    persistent:
      cluster.routing.allocation.cluster_concurrent_rebalance: 2
      cluster.routing.allocation.enable: "all"
      cluster.routing.allocation.node_concurrent_recoveries: 2
      cluster.blocks.read_only: false
      indices.lifecycle.poll_interval: "10m"
    # Transient settings are cleared on cluster restart
    transient:
      cluster.routing.allocation.enable: "none"
```

## Configuration

### ECK Automatic Discovery

When using Elastic Cloud on Kubernetes (ECK), the operator automatically discovers:
- Cluster endpoint URL
- Authentication credentials (elastic user)
- CA certificate for TLS verification

```yaml
spec:
  resourceSelector:
    name: elasticsearch  # ECK Elasticsearch resource name
    namespace: default   # Optional, defaults to CR namespace
```

### Manual Cluster Configuration

For non-ECK or external clusters, provide explicit connection details:

```yaml
spec:
  resourceSelector:
    endpoint: https://my-elasticsearch.example.com:9200
    username: elastic
    passwordSecretRef:
      name: es-credentials
      namespace: default
      key: password
    caCertSecretRef:  # Optional, skip TLS verification if omitted
      name: es-ca-cert
      namespace: default
      key: ca.crt
    clusterType: elasticsearch  # or "opensearch"
```

### Reconciliation Interval

Configure per-resource reconciliation frequency:

```yaml
spec:
  syncInterval: "5m"  # Accepts: "10s", "30s", "1m", "5m", "1h", etc.
```

Default: `10s`

## Elasticsearch vs OpenSearch

The operator automatically detects cluster type and validates CRD compatibility:

- **Elasticsearch**: Use `IndexLifecyclePolicy` for ILM
- **OpenSearch**: Use `IndexStateManagement` for ISM

All other resource types (`ClusterSettings`, `IndexTemplate`, `SnapshotLifecyclePolicy`, `SnapshotRepository`) are compatible with both platforms.

## Status Monitoring

View resource status with standard kubectl commands:

```bash
kubectl get indexlifecyclepolicies
```

```
NAME              PHASE   CLUSTER                      LAST SYNC            AGE
my-ilm-policies   Ready   default/elasticsearch        2025-01-02T11:00Z    5m
```

Detailed status information:

```bash
kubectl describe indexlifecyclepolicy my-ilm-policies
```

```yaml
Status:
  Phase: Ready
  Message: Successfully synced 2 policies
  Applied Resources:
    - hot-warm-cold
    - delete-after-30d
  Last Sync Time: 2025-01-02T11:00:00Z
  Target Cluster: default/elasticsearch
```

## Architecture

### Connection Management

The operator maintains a connection pool indexed by `<namespace>_<cluster-name>`. Connections feature:
- 10-second request timeout
- Persistent HTTP keep-alive
- Automatic TLS certificate verification
- Credential refresh on secret changes

### Reconciliation Flow

1. **Watch**: Observe Custom Resource changes
2. **Status Update**: Set phase to "Syncing"
3. **Connect**: Retrieve or create cluster connection from pool
4. **Detect Type**: Identify Elasticsearch vs OpenSearch
5. **Compare**: Diff desired state (CR spec) against applied state (CR status)
6. **Cleanup**: Remove resources deleted from CR spec
7. **Apply**: Synchronize all resources in CR spec to cluster
8. **Update Status**: Set phase to "Ready" with applied resource list and timestamp

### Resource Lifecycle

```
CR Created → Phase: Pending
    ↓
Connecting → Phase: Syncing
    ↓
Applying   → Phase: Syncing
    ↓
Success    → Phase: Ready
    ↓
Modified   → Re-reconcile
    ↓
Deleted    → Cleanup from cluster
```

## Development

### Prerequisites

- Kubebuilder v4.0.0+
- Go 1.24.6+
- Docker 17.03+
- kubectl 1.11.3+
- Kubernetes cluster (v1.11.3+)

### Local Development

Create a local Kubernetes cluster (using Kind or Minikube):

```bash
kind create cluster
```

Install CRDs and run the operator locally:

```bash
make install run
```

Apply sample resources:

```bash
kubectl apply -k config/samples/
```

### Building and Testing

```bash
# Run tests
make test

# Build binary
make build

# Build and push container image
export VERSION="0.1.0"
export IMG="ghcr.io/freepik-company/elastic-config-operator:v$VERSION"
make docker-build docker-push

# Deploy to cluster
make deploy IMG=$IMG
```

## RBAC Requirements

The operator requires the following Kubernetes permissions:

| Resource | Verbs | Purpose |
|----------|-------|---------|
| `secrets` | get, list, watch | Read cluster credentials and TLS certificates |
| `elasticsearches.elasticsearch.k8s.elastic.co` | get, list, watch | Discover ECK-managed Elasticsearch clusters |
| `indexlifecyclepolicies.elastic-config-operator.freepik.com` | * | Manage ILM CRs |
| `indexstatemanagements.elastic-config-operator.freepik.com` | * | Manage ISM CRs |
| `indextemplates.elastic-config-operator.freepik.com` | * | Manage Index Template CRs |
| `snapshotlifecyclepolicies.elastic-config-operator.freepik.com` | * | Manage SLM CRs |
| `snapshotrepositories.elastic-config-operator.freepik.com` | * | Manage Snapshot Repository CRs |
| `clustersettings.elastic-config-operator.freepik.com` | * | Manage Cluster Settings CRs |

## Troubleshooting

### View Operator Logs

```bash
kubectl logs -n elastic-config-operator deployment/elastic-config-operator-controller-manager -f
```

### Inspect Resource Status

```bash
kubectl describe indexlifecyclepolicy <name>
```

### Common Issues

**Connection Failures**
```
Error: failed to connect to Elasticsearch
```
- Verify cluster is running: `kubectl get elasticsearch`
- Check secret exists and contains valid credentials
- Verify network connectivity and firewall rules

**Cluster Type Mismatch**
```
Error: OpenSearch clusters use ISM instead of ILM
```
- Use `IndexStateManagement` for OpenSearch clusters
- Use `IndexLifecyclePolicy` for Elasticsearch clusters

**SLM Cron Format**
```
Error: invalid schedule: must be a valid cron expression
```
- Elasticsearch SLM requires 6-field cron format (includes seconds)
- Example: `0 0 1 * * ?` (1:00 AM daily)

**Status Stuck in Syncing**
- Check operator logs for detailed error messages
- Verify cluster accessibility and authentication
- Review timeout settings (default: 10s per request)

**TLS Certificate Verification**
```
Error: tls: failed to verify certificate
```
- Ensure `caCertSecretRef` is correctly configured
- For ECK, verify CR namespace matches cluster namespace
- Check certificate SANs match the endpoint hostname

## Release Process

1. Run test suite:
   ```bash
   make test
   ```

2. Set version and image:
   ```bash
   export VERSION="0.1.0"
   export IMG="ghcr.io/freepik-company/elastic-config-operator:v$VERSION"
   ```

3. Build and push image:
   ```bash
   make docker-build docker-push
   ```

4. Generate installation manifests:
   ```bash
   make build-installer
   ```

## Contributing

This project is built with [Kubebuilder](https://github.com/kubernetes-sigs/kubebuilder). Contributions are welcome via pull requests. All submissions require:

- Passing test suite (`make test`)
- Code review approval
- Adherence to Go best practices
- Clear commit messages describing changes

## Maintainers

- [Daniel Fradejas](https://github.com/dfradehubs) - fradejasdaniel@gmail.com

## License

Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
