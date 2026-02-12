# Zero-Downtime ACM Hub Upgrades: A Complete Guide to Managed Cluster Migration

## The Challenge: Upgrading ACM Hubs Without Disruption

Upgrading Red Hat Advanced Cluster Management (ACM) hub clusters has traditionally been a high-risk operation. In-place upgrades can lead to:

- Extended maintenance windows affecting hundreds of managed clusters
- Risk of upgrade failures leaving hubs in inconsistent states
- Potential disruption to policy enforcement and governance
- Downtime for critical management operations

For organizations running large-scale deployments with hundreds of ZTP (Zero Touch Provisioning) managed clusters, the stakes are even higher. A failed upgrade can impact production workloads across your entire fleet.

## The Solution: Parallel Hub Deployment with Cluster Migration

Multicluster Global Hub introduces **Managed Cluster Migration**, a feature that fundamentally changes how you approach ACM hub upgrades. Instead of risky in-place upgrades, you can:

1. **Deploy a new ACM hub** with your target version in parallel
2. **Gradually migrate managed clusters** from the old hub to the new hub
3. **Validate each migration batch** before proceeding
4. **Decommission the old hub** only after full validation

This approach reduces upgrade risk to near zero while maintaining continuous management of your clusters.

## How It Works: The Migration Architecture

The migration process leverages Global Hub as an orchestration layer between source and target hubs:

![MCM Architecture](https://github.com/ldpliu/multicluster-global-hub/blob/main/doc/images/mcm-arch.png)


### What Gets Migrated Automatically

The migration process transfers all essential resources:

- **ManagedCluster** and **KlusterletAddonConfig**: Cluster registration and add-on configurations
- **ClusterDeployment** and **ImageClusterInstall**: Deployment configurations with status preserved
- **BareMetalHost** resources: Physical server inventory and BIOS configurations
- **Secrets**: Admin credentials, kubeconfig, BMC credentials, pull secrets
- **ConfigMaps**: Extra manifests referenced by ImageClusterInstall

### Migration Phases

Each migration progresses through well-defined phases:

| Phase | Description |
|-------|-------------|
| Validating | Verifies clusters and hubs are valid |
| Initializing | Prepares target hub (kubeconfig, RBAC) and source hub (KlusterletConfig) |
| Deploying | Transfers resources to target hub |
| Registering | Re-registers clusters with target hub |
| Cleaning | Removes resources from source hub |
| Completed | Migration finished successfully |

## Prerequisites

Before starting the migration, ensure both source and target hubs meet the following requirements.

### 1. Network Connectivity

All managed clusters to be migrated **must have network connectivity** to the target hub cluster.

### 2. Version Requirements

| Component | Source Hub | Target Hub | Notes |
|-----------|------------|------------|-------|
| **ACM** | Version N | Version N to N+1, and EUS Version support | e.g., ACM 2.13 → 2.15 |
| **OpenShift** | 4.x | 4.x | Should be compatible with ACM version |
| **Global Hub** | N/A | Latest stable | Installed on target hub only |

### 3. Target Hub Configuration

The target hub must be configured with the **same components** in `MultiClusterEngine` and `MultiClusterHub` as the source hub to ensure successful cluster migration.

For ZTP clusters, ensure the siteconfig operator is enabled:

```bash
oc patch multiclusterhub multiclusterhub -n open-cluster-management --type=merge -p '
spec:
  overrides:
    components:
      - name: siteconfig
        enabled: true
'
```

### 4. Global Hub Setup

Install Global Hub on the target hub and import the source hub:

```bash
# Verify Global Hub is running on target hub
oc get mgh -n multicluster-global-hub
oc get pods -n multicluster-global-hub
```

> **Important**: When importing the source hub, the label `global-hub.open-cluster-management.io/deploy-mode: hosted` is **required** for migration to work.

---

## Migration Process

The migration process consists of five steps: pre-migration verification, applying policy applications, creating the migration resource, monitoring progress, and post-migration validation.

### Step 1: Pre-Migration Verification

Before initiating migration, verify the current state of your clusters on the **source hub**.

#### 1.1 Check Cluster Status

```bash
# List all managed clusters on source hub
oc get managedcluster

# Expected output:
# NAME       HUB ACCEPTED   MANAGED CLUSTER URLS   JOINED   AVAILABLE   AGE
# cluster-001   true                                  True     True        30d
# cluster-002   true                                  True     True        25d
```

#### 1.2 Verify ZTP Resources (for ZTP clusters)

```bash
# Check ClusterInstance status
oc get clusterinstance -A

# Check ImageClusterInstall status
oc get imageclusterinstall -A

# Check ClusterDeployment status
oc get clusterdeployment -A
```

> **Important**: Only migrate clusters with `PROVISIONSTATUS: Completed`. Clusters still provisioning should complete before migration.

### Step 2: Apply Policy Applications to Target Hub

> **Critical Step**: Policy applications must be applied to the target hub **BEFORE** creating the migration resource.

#### 2.1 Export from Source Hub

```bash
oc get application -n openshift-gitops -l app=policies -o yaml > policies-app.yaml
```

#### 2.2 Apply to Target Hub

```bash
oc apply -f policies-app.yaml

# Verify policies are created
oc get policy -A
```

### Step 3: Create Migration Resource

Migrate clusters in batches for controlled rollout.

#### 3.1 Static Cluster List

Use this approach when you have a specific list of clusters:

```yaml
apiVersion: global-hub.open-cluster-management.io/v1alpha1
kind: ManagedClusterMigration
metadata:
  name: upgrade-migration-batch-1
  namespace: multicluster-global-hub
spec:
  from: source-hub
  to: local-cluster
  includedManagedClusters:
    - cluster-001
    - cluster-002
    - cluster-003
  supportedConfigs:
    stageTimeout: 15m
```

#### 3.2 Dynamic Selection with Placement

For large-scale migrations, use Placement for dynamic cluster selection:

```yaml
apiVersion: cluster.open-cluster-management.io/v1beta1
kind: Placement
metadata:
  name: migration-batch-300
spec:
  numberOfClusters: 300
  clusterSets:
  - global
  predicates:
  - requiredClusterSelector:
      labelSelector:
        matchExpressions:
          - key: migration-batch
            operator: In
            values: ["1"]
---
apiVersion: global-hub.open-cluster-management.io/v1alpha1
kind: ManagedClusterMigration
metadata:
  name: upgrade-migration-batch-1
  namespace: multicluster-global-hub
spec:
  from: source-hub
  to: local-cluster
  includedManagedClustersPlacementRef: migration-batch-300
  supportedConfigs:
    stageTimeout: 15m
```

### Step 4: Monitor Migration Progress

#### 4.1 Check Migration Phase

```bash
oc get managedclustermigration -n multicluster-global-hub

# Example output:
# NAME                        PHASE        AGE
# upgrade-migration-batch-1   Deploying    5m
```

**Migration Phases**:

| Phase | Description |
|-------|-------------|
| Validating | Verifies clusters and hubs are valid |
| Initializing | Prepares target hub and source hub |
| Deploying | Transfers resources to target hub |
| Registering | Re-registers clusters with target hub |
| Cleaning | Removes resources from source hub |
| Completed | Migration finished successfully |

#### 4.2 Monitor Individual Cluster Status

```bash
# View per-cluster status via ConfigMap
oc get configmap upgrade-migration-batch-1 -n multicluster-global-hub -o yaml
```

### Step 5: Post-Migration Validation

#### 5.1 Verify Cluster Availability

```bash
oc get managedcluster
```

#### 5.2 Verify Policy Compliance

```bash
oc get policy -A
```

#### 5.3 Apply ClusterInstance Applications (for ZTP clusters)

> **Important**: ClusterInstance resources are managed by GitOps and are **NOT automatically migrated**.

**Step 1**: Update ClusterInstance resources in your Git repository to suppress re-rendering:

```yaml
spec:
  suppressedManifests:
    - BareMetalHost
    - ImageClusterInstall
```

**Step 2**: Apply ClusterInstance applications to target hub:

```bash
oc apply -f clusterinstance-app.yaml
```

**Step 3**: Verify ClusterInstance status:

```bash
oc get clusterinstance -A
```

#### 5.4 Repeat for Remaining Batches

After validating each batch, repeat Steps 3-5 for remaining cluster batches.

## Performance at Scale: Real-World Results

We tested migration with 300 ZTP-managed SNO clusters:

| Scenario | Migration Time | Policy Convergence | ClusterInstance Convergence |
|----------|---------------|-------------------|----------------------------|
| ACM 2.15 → 2.15 | 4 min 40 sec | ~2 minutes | ~2 minutes |
| ACM 2.13 → 2.15 | ~9 minutes | <2 minutes | <2 minutes |

**Key findings:**
- **100% success rate** for all 300 clusters
- **~0.93 seconds per cluster** in same-version migrations
- **No cluster downtime** during migration
- **Automatic rollback** when failures occur

## Built-in Safety: Automatic Rollback

If migration fails during any phase, Global Hub automatically rolls back:

```
Deploying (failure) → Rollbacking → Failed
```

Rollback ensures:
- Clusters remain operational on source hub
- Partial resources on target hub are cleaned up
- System returns to pre-migration state

In our testing, rollback of 300 clusters completed within 5 minutes.

## Summary

Managed Cluster Migration transforms ACM hub upgrades from high-risk operations to controlled, reversible processes:

- **Deploy new versions in parallel** - no in-place upgrade risk
- **Migrate incrementally** - validate before proceeding
- **Automatic rollback** - failures don't leave clusters stranded
- **Zero downtime** - clusters remain managed throughout

For organizations managing hundreds or thousands of clusters, this approach provides the confidence to upgrade ACM hubs without fear of disruption.

---

## Getting Started

To explore Managed Cluster Migration:

1. Review the [Global Hub Cluster Migration Guide](https://github.com/ldpliu/multicluster-global-hub/blob/main/doc/migration/global_hub_cluster_migration.md)
2. For ZTP clusters, see [ClusterInstance ZTP Migration Guide](https://github.com/ldpliu/multicluster-global-hub/blob/main/doc/migration/clusterinstance_ztp_migration.md)
3. Check [Migration Performance](https://github.com/ldpliu/multicluster-global-hub/blob/main/doc/migration/migration_performance.md) for scale testing results
