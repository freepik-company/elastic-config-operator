# Elastic Config Operator Helm Chart

A Helm chart for deploying the Elastic Config Operator to Kubernetes.

## TL;DR

```bash
helm repo add freepik https://charts.freepik.com
helm install elastic-config-operator freepik/elastic-config-operator
```

## Introduction

This chart bootstraps an [Elastic Config Operator](https://github.com/freepik-company/elastic-config-operator) deployment on a Kubernetes cluster using the Helm package manager.

## Prerequisites

- Kubernetes 1.11.3+
- Helm 3+
- (Optional) [Elastic Cloud on Kubernetes (ECK)](https://www.elastic.co/guide/en/cloud-on-k8s/current/k8s-install-helm.html)

## Installing the Chart

### Standard Installation

```bash
helm install my-release freepik/elastic-config-operator
```

This command deploys the Elastic Config Operator with default configuration. The [Parameters](#parameters) section lists the parameters that can be configured during installation.

### Installation Without CRDs

If CRDs are already installed or you want to manage them separately:

```bash
helm install my-release freepik/elastic-config-operator --skip-crds
```

### Upgrading

```bash
# Standard upgrade (CRDs are NOT updated by default in Helm)
helm upgrade my-release freepik/elastic-config-operator

# To update CRDs manually before upgrading:
kubectl apply -f https://raw.githubusercontent.com/freepik-company/elastic-config-operator/main/config/crd/bases/
helm upgrade my-release freepik/elastic-config-operator
```

**Note**: Helm does not upgrade CRDs automatically. You must update them manually if the CRD definitions change.

## Uninstalling the Chart

```bash
helm uninstall my-release
```

This command removes all the Kubernetes components associated with the chart and deletes the release.

**Important**: By default, CRDs are configured with `helm.sh/resource-policy: keep`, which means they will **NOT be deleted** when you uninstall the chart. This prevents accidental data loss.

To also remove CRDs (⚠️ this will delete all your custom resources):

```bash
kubectl delete crd indexlifecyclepolicies.elastic-config-operator.freepik.com
kubectl delete crd indextemplates.elastic-config-operator.freepik.com
kubectl delete crd snapshotlifecyclepolicies.elastic-config-operator.freepik.com
kubectl delete crd snapshotrepositories.elastic-config-operator.freepik.com
```

## Parameters

### Global parameters

| Name | Description | Value |
|------|-------------|-------|
| `nameOverride` | String to partially override the fullname template | `""` |
| `fullnameOverride` | String to fully override the fullname template | `""` |

### CRD parameters

| Name | Description | Value |
|------|-------------|-------|
| `crds.install` | Install CRDs as part of the chart | `true` |
| `crds.keep` | Keep CRDs on uninstall (prevents data loss) | `true` |

### Controller parameters

| Name | Description | Value |
|------|-------------|-------|
| `controller.replicaCount` | Number of replicas | `1` |
| `controller.image.repository` | Image repository | `ghcr.io/freepik-company/elastic-config-operator` |
| `controller.image.pullPolicy` | Image pull policy | `IfNotPresent` |
| `controller.image.tag` | Image tag (defaults to chart appVersion) | `""` |
| `controller.imagePullSecrets` | Image pull secrets | `[]` |

### ServiceAccount parameters

| Name | Description | Value |
|------|-------------|-------|
| `controller.serviceAccount.create` | Create service account | `true` |
| `controller.serviceAccount.annotations` | Service account annotations | `{}` |
| `controller.serviceAccount.name` | Service account name | `elastic-config-operator-controller-manager` |

### RBAC parameters

| Name | Description | Value |
|------|-------------|-------|
| `controller.rbac.clusterWideSecrets` | Grant cluster-wide secret read permissions | `true` |
| `controller.rbac.secretsNamespaces` | List of namespaces for secret read permissions (if clusterWideSecrets is false) | `[]` |

### Security parameters

| Name | Description | Value |
|------|-------------|-------|
| `controller.podSecurityContext.runAsNonRoot` | Run as non-root user | `true` |
| `controller.securityContext.allowPrivilegeEscalation` | Allow privilege escalation | `false` |
| `controller.securityContext.capabilities.drop` | Drop capabilities | `["ALL"]` |

### Resource parameters

| Name | Description | Value |
|------|-------------|-------|
| `controller.resources.limits.cpu` | CPU limit | `500m` |
| `controller.resources.limits.memory` | Memory limit | `512Mi` |
| `controller.resources.requests.cpu` | CPU request | `100m` |
| `controller.resources.requests.memory` | Memory request | `128Mi` |

### Autoscaling parameters

| Name | Description | Value |
|------|-------------|-------|
| `controller.autoscaling.enabled` | Enable autoscaling | `false` |
| `controller.autoscaling.minReplicas` | Minimum replicas | `1` |
| `controller.autoscaling.maxReplicas` | Maximum replicas | `10` |
| `controller.autoscaling.targetCPUUtilizationPercentage` | Target CPU utilization | `80` |
| `controller.autoscaling.targetMemoryUtilizationPercentage` | Target memory utilization | `80` |

### Metrics parameters

| Name | Description | Value |
|------|-------------|-------|
| `controller.metrics.enabled` | Enable metrics | `true` |
| `controller.metrics.service.enabled` | Enable metrics service | `true` |
| `controller.metrics.service.type` | Metrics service type | `ClusterIP` |
| `controller.metrics.service.port` | Metrics service port | `8443` |

## Configuration Examples

### Cluster-wide secret access (default)

```yaml
controller:
  rbac:
    clusterWideSecrets: true
```

### Namespace-specific secret access

```yaml
controller:
  rbac:
    clusterWideSecrets: false
    secretsNamespaces:
      - default
      - elasticsearch
      - prod
```

This creates Role and RoleBinding for each namespace, allowing the operator to read secrets only in those specific namespaces.

### Custom resources

```yaml
controller:
  resources:
    limits:
      cpu: 1000m
      memory: 1Gi
    requests:
      cpu: 200m
      memory: 256Mi
```

### Enable autoscaling

```yaml
controller:
  autoscaling:
    enabled: true
    minReplicas: 2
    maxReplicas: 10
    targetCPUUtilizationPercentage: 70
```

## Supported CRDs

The operator manages the following Custom Resource Definitions:

- **IndexLifecyclePolicy** - Elasticsearch ILM policies
- **IndexTemplate** - Elasticsearch index templates
- **SnapshotLifecyclePolicy** - Elasticsearch SLM policies
- **SnapshotRepository** - Elasticsearch snapshot repositories

See the [main README](https://github.com/freepik-company/elastic-config-operator) for usage examples.

## CRD Management

### Why CRDs are kept on uninstall

By default, CRDs have the annotation `helm.sh/resource-policy: keep`. This means:
- ✅ Your custom resources (ILM policies, templates, etc.) are preserved on uninstall
- ✅ Prevents accidental data loss
- ✅ CRDs can be shared across multiple releases

### Handling CRD conflicts

If you encounter "CRD already exists" errors:

```bash
# Option 1: Skip CRDs during installation
helm install my-release freepik/elastic-config-operator --skip-crds

# Option 2: Upgrade existing installation
helm upgrade my-release freepik/elastic-config-operator

# Option 3: Delete and reinstall (⚠️ will delete all custom resources)
kubectl delete crd indexlifecyclepolicies.elastic-config-operator.freepik.com
helm install my-release freepik/elastic-config-operator
```

### Updating CRDs

Helm does not update CRDs on `helm upgrade`. To update CRDs:

```bash
# Method 1: Apply directly from repository
kubectl apply -f https://raw.githubusercontent.com/freepik-company/elastic-config-operator/v1.0.0/config/crd/bases/

# Method 2: From local chart
kubectl apply -f charts/elastic-config-operator/crds/

# Then upgrade the release
helm upgrade my-release freepik/elastic-config-operator
```

## Troubleshooting

### Operator cannot access secrets

If you see permission errors for reading secrets:

1. Check if `clusterWideSecrets` is enabled, or
2. Ensure the namespace is listed in `secretsNamespaces`

### Operator cannot find Elasticsearch resources

Ensure the operator has permissions to read ECK Elasticsearch resources. This is automatically included in the ClusterRole.

## Contributing

Contributions are welcome! Please open an issue or pull request on [GitHub](https://github.com/freepik-company/elastic-config-operator).

## License

Apache 2.0 License

