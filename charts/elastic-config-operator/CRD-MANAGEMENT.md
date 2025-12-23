# CRD Management Guide

This guide explains how to manage Custom Resource Definitions (CRDs) with the Elastic Config Operator Helm chart.

## Understanding CRD Installation

### Default Behavior

The Helm chart installs CRDs automatically with these characteristics:

1. **Location**: CRDs are in the `crds/` directory
2. **Annotation**: `helm.sh/resource-policy: keep`
3. **Effect**: CRDs are:
   - Installed on `helm install`
   - **NOT updated** on `helm upgrade` (Helm limitation)
   - **NOT deleted** on `helm uninstall` (due to `keep` annotation)

This prevents accidental data loss of your Elasticsearch configurations.

## Installation Scenarios

### Scenario 1: Fresh Installation

```bash
helm install elastic-config-operator freepik/elastic-config-operator
```

✅ Everything is installed including CRDs.

### Scenario 2: CRDs Already Exist

If CRDs are already installed (from a previous installation):

```bash
# Option A: Skip CRDs (Helm will not try to install them)
helm install elastic-config-operator freepik/elastic-config-operator --skip-crds

# Option B: Let Helm handle it (may show warnings but won't fail)
helm install elastic-config-operator freepik/elastic-config-operator
```

### Scenario 3: Upgrading the Chart

```bash
# CRDs are NOT upgraded automatically
helm upgrade elastic-config-operator freepik/elastic-config-operator

# To update CRDs, apply them manually first:
kubectl apply -f crds/
helm upgrade elastic-config-operator freepik/elastic-config-operator
```

### Scenario 4: Managing CRDs Separately

If you want full control over CRDs:

```bash
# Install CRDs manually
kubectl apply -f crds/

# Install chart without CRDs
helm install elastic-config-operator freepik/elastic-config-operator \
  --set crds.install=false
```

## Common Issues & Solutions

### Issue 1: "CRD already exists"

**Error:**
```
Error: rendered manifests contain a resource that already exists
```

**Solution:**
```bash
# Use --skip-crds flag
helm install elastic-config-operator freepik/elastic-config-operator --skip-crds
```

### Issue 2: CRDs Not Updating

**Problem:** New CRD fields not appearing after `helm upgrade`

**Solution:**
```bash
# Update CRDs manually
kubectl apply -f https://raw.githubusercontent.com/freepik-company/elastic-config-operator/v1.0.0/config/crd/bases/

# OR from local chart
kubectl apply -f charts/elastic-config-operator/crds/

# Then upgrade
helm upgrade elastic-config-operator freepik/elastic-config-operator
```

### Issue 3: Removing CRDs and All Data

**⚠️ WARNING:** This will delete all your ILM policies, templates, etc.

```bash
# 1. Uninstall the chart
helm uninstall elastic-config-operator

# 2. Delete all custom resources first (optional, to avoid orphans)
kubectl delete indexlifecyclepolicies --all --all-namespaces
kubectl delete indextemplates --all --all-namespaces
kubectl delete snapshotlifecyclepolicies --all --all-namespaces
kubectl delete snapshotrepositories --all --all-namespaces

# 3. Delete CRDs
kubectl delete crd indexlifecyclepolicies.elastic-config-operator.freepik.com
kubectl delete crd indextemplates.elastic-config-operator.freepik.com
kubectl delete crd snapshotlifecyclepolicies.elastic-config-operator.freepik.com
kubectl delete crd snapshotrepositories.elastic-config-operator.freepik.com
```

### Issue 4: CRD Version Mismatch

**Problem:** Operator fails with "unknown field" errors

**Cause:** CRD version doesn't match operator version

**Solution:**
```bash
# Check CRD version
kubectl get crd indexlifecyclepolicies.elastic-config-operator.freepik.com -o yaml | grep controller-gen

# Update to match operator version
kubectl apply -f https://raw.githubusercontent.com/freepik-company/elastic-config-operator/v1.0.0/config/crd/bases/
```

## Best Practices

### Production Environments

1. **Manage CRDs separately** from the Helm chart:
   ```bash
   # In GitOps or CI/CD pipeline:
   kubectl apply -f crds/
   helm upgrade elastic-config-operator freepik/elastic-config-operator --skip-crds
   ```

2. **Version CRDs explicitly**:
   ```bash
   kubectl apply -f https://raw.githubusercontent.com/freepik-company/elastic-config-operator/v1.0.0/config/crd/bases/
   ```

3. **Test CRD updates** in staging first

### Development Environments

1. **Let Helm manage everything**:
   ```bash
   helm install elastic-config-operator freepik/elastic-config-operator
   ```

2. **Clean installs** are OK:
   ```bash
   helm uninstall elastic-config-operator
   kubectl delete crds -l app.kubernetes.io/name=elastic-config-operator
   helm install elastic-config-operator freepik/elastic-config-operator
   ```

## Verification

### Check if CRDs are installed

```bash
kubectl get crds | grep elastic-config-operator
```

Expected output:
```
indexlifecyclepolicies.elastic-config-operator.freepik.com
indextemplates.elastic-config-operator.freepik.com
snapshotlifecyclepolicies.elastic-config-operator.freepik.com
snapshotrepositories.elastic-config-operator.freepik.com
```

### Check CRD version

```bash
kubectl get crd indexlifecyclepolicies.elastic-config-operator.freepik.com -o jsonpath='{.metadata.annotations.controller-gen\.kubebuilder\.io/version}'
```

### Verify CRD annotations

```bash
kubectl get crd indexlifecyclepolicies.elastic-config-operator.freepik.com -o jsonpath='{.metadata.annotations.helm\.sh/resource-policy}'
```

Should output: `keep`

## Helm Chart Configuration

### Disable CRD installation

```yaml
crds:
  install: false
```

### Allow CRD deletion on uninstall (not recommended)

This requires modifying the CRDs to remove the `helm.sh/resource-policy: keep` annotation.

## Migration Strategies

### From Manual CRD Management to Helm

```bash
# 1. CRDs are already installed manually
# 2. Install chart without CRDs first
helm install elastic-config-operator freepik/elastic-config-operator --skip-crds

# 3. Later, you can let Helm adopt them
kubectl annotate crd indexlifecyclepolicies.elastic-config-operator.freepik.com meta.helm.sh/release-name=elastic-config-operator
kubectl annotate crd indexlifecyclepolicies.elastic-config-operator.freepik.com meta.helm.sh/release-namespace=default
# (repeat for other CRDs)
```

### From Helm to Manual Management

```bash
# 1. Remove Helm annotations
kubectl annotate crd indexlifecyclepolicies.elastic-config-operator.freepik.com meta.helm.sh/release-name-
kubectl annotate crd indexlifecyclepolicies.elastic-config-operator.freepik.com meta.helm.sh/release-namespace-
# (repeat for other CRDs)

# 2. Upgrade chart to not manage CRDs
helm upgrade elastic-config-operator freepik/elastic-config-operator \
  --set crds.install=false \
  --skip-crds
```

## Summary

| Scenario | Command | CRDs Installed? | CRDs Updated? | CRDs Deleted on Uninstall? |
|----------|---------|-----------------|---------------|----------------------------|
| Fresh install | `helm install` | ✅ Yes | N/A | ❌ No (kept) |
| Upgrade | `helm upgrade` | N/A | ❌ No | ❌ No (kept) |
| Install with existing CRDs | `helm install --skip-crds` | ❌ No | N/A | ❌ No (kept) |
| Uninstall | `helm uninstall` | N/A | N/A | ❌ No (kept) |
| Manual CRD update | `kubectl apply -f crds/` | ✅ Yes | ✅ Yes | N/A |

**Key Takeaway:** CRDs are never deleted automatically. This is intentional to protect your data.

