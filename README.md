# ECK Config Operator

A Kubernetes operator to manage Elasticsearch and OpenSearch configuration (ILM/ISM policies, Index Templates, Snapshot Lifecycle Policies, Snapshot Repositories, and Cluster Settings) as Kubernetes Custom Resources.

## Overview

The ECK Config Operator simplifies the management of Elasticsearch configuration by allowing you to define and manage Elasticsearch resources declaratively using Kubernetes Custom Resources. It works seamlessly with [Elastic Cloud on Kubernetes (ECK)](https://www.elastic.co/guide/en/cloud-on-k8s/current/index.html) or any standalone Elasticsearch cluster.

### Features

- ✅ **Declarative Configuration**: Manage Elasticsearch configuration as Kubernetes resources
- ✅ **Automatic ECK Integration**: Automatically discovers Elasticsearch endpoints and credentials from ECK
- ✅ **Manual Configuration Support**: Connect to any Elasticsearch cluster (not just ECK)
- ✅ **Resource Tracking**: Track applied resources and detect configuration drift
- ✅ **Automatic Cleanup**: Delete Elasticsearch resources when Kubernetes CR is deleted
- ✅ **Status Reporting**: Rich status reporting with phases (Pending, Syncing, Ready, Error)
- ✅ **Configurable Sync Interval**: Control how often resources are reconciled
- ✅ **Connection Pooling**: Efficient connection reuse across reconciliations

### Supported Resources

| Custom Resource | Elasticsearch API | OpenSearch API | Notes |
|----------------|-------------------|----------------|-------|
| `ClusterSettings` | ✅ Cluster Settings | ✅ Cluster Settings | Fully compatible |
| `IndexLifecyclePolicy` | ✅ Index Lifecycle Management (ILM) | ❌ Not supported | Elasticsearch only |
| `IndexStateManagement` | ❌ Not supported | ✅ Index State Management (ISM) | OpenSearch only |
| `IndexTemplate` | ✅ Index Templates | ✅ Index Templates | Fully compatible |
| `SnapshotLifecyclePolicy` | ✅ Snapshot Lifecycle Management (SLM) | ✅ Snapshot Lifecycle Management (SLM) | Fully compatible |
| `SnapshotRepository` | ✅ Snapshot Repositories | ✅ Snapshot Repositories | Fully compatible |

## Getting Started

### Prerequisites

- Kubernetes v1.11.3+
- kubectl v1.11.3+
- Go v1.24.6+ (for development)
- Docker 17.03+ (for building images)
- [Elastic Cloud on Kubernetes (ECK)](https://www.elastic.co/guide/en/cloud-on-k8s/current/k8s-install-helm.html) (recommended) or standalone Elasticsearch

### Installation

#### Option 1: Using Kustomize

1. **Install the CRDs:**

```bash
kubectl apply -f https://raw.githubusercontent.com/<org>/eck-config-operator/<tag>/config/crd/bases/
```

2. **Deploy the operator:**

```bash
kubectl apply -k config/default
```

#### Option 2: From source

1. **Clone the repository:**

```bash
git clone https://github.com/<org>/eck-config-operator.git
cd eck-config-operator
```

2. **Install CRDs:**

```bash
make install
```

3. **Deploy the operator:**

```bash
make deploy IMG=<your-registry>/eck-config-operator:tag
```

### Quick Start Example

#### 1. Create an Elasticsearch cluster (using ECK)

```bash
kubectl apply -f config/samples/elasticsearch_sample.yaml
```

#### 2. Create an Index Lifecycle Policy

```yaml
apiVersion: eck-config-operator.freepik.com/v1alpha1
kind: IndexLifecyclePolicy
metadata:
  name: my-ilm-policies
spec:
  syncInterval: "30s"  # Optional, defaults to 10s
  resourceSelector:
    name: elasticsearch  # Name of the ECK Elasticsearch resource
    namespace: default   # Optional, defaults to resource namespace
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

#### 3. Check the status

```bash
kubectl get indexlifecyclepolicies
```

```
NAME              PHASE   LAST SYNC            AGE
my-ilm-policies   Ready   2025-12-23T11:00Z    5m
```

```bash
kubectl describe indexlifecyclepolicy my-ilm-policies
```

## Configuration

### ECK Automatic Configuration

When using ECK, the operator automatically discovers:
- Elasticsearch endpoint
- Credentials (elastic user)
- CA certificate

```yaml
spec:
  resourceSelector:
    name: elasticsearch  # ECK Elasticsearch resource name
    namespace: default   # Optional
```

### Manual Elasticsearch Configuration

For non-ECK or external Elasticsearch clusters:

```yaml
spec:
  resourceSelector:
    endpoint: https://my-elasticsearch.example.com:9200
    username: elastic
    passwordSecretRef:
      name: es-credentials
      namespace: default
      key: password
    caCertSecretRef:  # Optional, skip TLS verification if not provided
      name: es-ca-cert
      namespace: default
      key: ca.crt
```

### Sync Interval

Control how often the operator reconciles resources:

```yaml
spec:
  syncInterval: "5m"  # Supports: "10s", "30s", "1m", "5m", "1h", etc.
```

## Custom Resource Examples

### Index Template

```yaml
apiVersion: eck-config-operator.freepik.com/v1alpha1
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
      template:
        settings:
          number_of_shards: 1
          number_of_replicas: 1
          index.lifecycle.name: "30d-retention"
        mappings:
          properties:
            "@timestamp":
              type: date
            message:
              type: text
```

### Snapshot Repository

```yaml
apiVersion: eck-config-operator.freepik.com/v1alpha1
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

```yaml
apiVersion: eck-config-operator.freepik.com/v1alpha1
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

```yaml
apiVersion: eck-config-operator.freepik.com/v1alpha1
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
    # Transient settings don't survive cluster restarts (useful for maintenance)
    transient:
      # Temporarily disable shard allocation during maintenance
      cluster.routing.allocation.enable: "none"
```

### Index State Management (OpenSearch only)

⚠️ **Note**: This resource is specifically for OpenSearch clusters. For Elasticsearch, use `IndexLifecyclePolicy` instead.

```yaml
apiVersion: eck-config-operator.freepik.com/v1alpha1
kind: IndexStateManagement
metadata:
  name: my-ism-policies
spec:
  resourceSelector:
    name: opensearch
    clusterType: opensearch  # REQUIRED for OpenSearch
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

## Elasticsearch vs OpenSearch

The operator automatically detects the cluster type (Elasticsearch or OpenSearch) and validates that you're using the correct CRD:

- **Elasticsearch clusters**: Use `IndexLifecyclePolicy` for ILM (Index Lifecycle Management)
- **OpenSearch clusters**: Use `IndexStateManagement` for ISM (Index State Management)

All other resources (`ClusterSettings`, `IndexTemplate`, `SnapshotLifecyclePolicy`, `SnapshotRepository`) work with both Elasticsearch and OpenSearch.

### Manual cluster type configuration

```yaml
spec:
  resourceSelector:
    name: my-cluster
    clusterType: opensearch  # or "elasticsearch" - overrides auto-detection
```

## Status Fields

All resources report their status:

```yaml
status:
  phase: Ready  # Pending, Syncing, Ready, or Error
  message: "Successfully synced 2 policies"
  appliedResources:
    - hot-warm-cold
    - delete-after-30d
  lastSyncTime: "2025-12-23T11:00:00Z"
```

## Development

### Prerequisites

- Go 1.24.6+
- Docker
- kubectl
- kind or minikube (for local testing)

### Local Development Setup

1. **Install ECK and Elasticsearch:**

```bash
make install-eck
make install-elasticsearch
```

2. **Setup local development environment:**

```bash
make setup-local-dev
```

This will:
- Add Elasticsearch service to `/etc/hosts`
- Forward Elasticsearch port to localhost:9200

3. **Run the operator locally:**

```bash
make run
```

4. **Cleanup:**

```bash
make cleanup-local-dev
```

### Building

```bash
# Build the operator
make build

# Build and push Docker image
make docker-build docker-push IMG=<registry>/eck-config-operator:tag

# Deploy to cluster
make deploy IMG=<registry>/eck-config-operator:tag
```

### Testing

```bash
# Run unit tests
make test

# Run with coverage
make test-coverage

# Apply sample resources
kubectl apply -f config/samples/
```

## RBAC Permissions

The operator requires the following permissions:

| Resource | Verbs | Reason |
|----------|-------|--------|
| `secrets` | get, list, watch | Read Elasticsearch credentials and CA certificates |
| `elasticsearches.elasticsearch.k8s.elastic.co` | get, list, watch | Discover ECK Elasticsearch resources |
| `indexlifecyclepolicies.eck-config-operator.freepik.com` | * | Manage ILM policy CRs |
| `indextemplates.eck-config-operator.freepik.com` | * | Manage Index Template CRs |
| `snapshotlifecyclepolicies.eck-config-operator.freepik.com` | * | Manage SLM policy CRs |
| `snapshotrepositories.eck-config-operator.freepik.com` | * | Manage Snapshot Repository CRs |

## Architecture

### Connection Management

The operator maintains a connection pool to Elasticsearch clusters. Connections are:
- Created on first use
- Cached with a key of `<namespace>_<cluster-name>`
- Reused across reconciliation loops
- Include 10-second timeouts for reliability

### Reconciliation Flow

1. **Watch**: Operator watches for CR changes
2. **Sync Status**: Update status to "Syncing"
3. **Connect**: Get or create Elasticsearch connection
4. **Compare**: Compare desired state (CR) with actual state (Status)
5. **Delete**: Remove resources no longer in CR
6. **Apply**: Apply all desired resources (idempotent)
7. **Update Status**: Set status to "Ready" with applied resources

### Resource Lifecycle

```
CR Created → Status: Pending
           ↓
    Connecting to ES → Status: Syncing
           ↓
    Applying Config → Status: Syncing
           ↓
    Success → Status: Ready
           ↓
    CR Modified → Re-sync
           ↓
    CR Deleted → Delete from ES
```

## Troubleshooting

### Operator logs

```bash
kubectl logs -n eck-config-operator-system deployment/eck-config-operator-controller-manager -f
```

### Check resource status

```bash
kubectl describe indexlifecyclepolicy <name>
```

### Common Issues

**Error: "failed to connect to Elasticsearch"**
- Verify Elasticsearch is running: `kubectl get elasticsearch`
- Check credentials secret exists
- Verify network connectivity

**Error: "unknown field 'config'"**
- For SLM policies, use 6-field cron format: `0 0 1 * * ?`
- Verify JSON structure matches Elasticsearch API

**Status stuck in "Syncing"**
- Check operator logs for errors
- Verify Elasticsearch is accessible
- Check timeout settings (default 10s)

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

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
