# Control Plane Load Balancing with Managed Cluster Migration

## The Growing Pains of Large-Scale Cluster Management

As organizations scale their Kubernetes deployments, ACM hub clusters can become overwhelmed. A single hub managing hundreds of clusters faces increasing pressure:

- **Resource exhaustion**: CPU and memory limits on the hub cluster
- **API server latency**: Slow responses affecting policy enforcement
- **Uneven distribution**: Some hubs overloaded while others sit idle
- **Scaling bottlenecks**: Unable to add more clusters to capacity-constrained hubs

Traditional solutions require either expensive vertical scaling or complex hub redeployments.

## A New Approach: Dynamic Cluster Redistribution

Multicluster Global Hub enables **control plane load balancing** through Managed Cluster Migration. You can now:

1. **Move clusters between hubs** based on capacity needs
2. **Balance workloads** across multiple hub clusters
3. **Scale horizontally** by adding new hubs and redistributing clusters
4. **Optimize resources** by consolidating underutilized hubs

## Architecture for Multi-Hub Load Balancing

![MCM Architecture](https://github.com/ldpliu/multicluster-global-hub/blob/main/doc/images/mcm-load-balance.png)

Global Hub provides the coordination layer to move clusters between hubs while maintaining cluster state and management continuity.

## Prerequisites

Before implementing control plane load balancing, you need to set up the Global Hub environment:

### 1. Install Global Hub on Target Hub

Install the Multicluster Global Hub operator on your **target hub** (the ACM hub that will coordinate migrations):

```bash
# Verify Global Hub is running
oc get mgh -n multicluster-global-hub
oc get pods -n multicluster-global-hub
```

For detailed installation instructions, see the [Global Hub Installation Guide](https://github.com/stolostron/multicluster-global-hub#run-the-operator-in-the-cluster).

### 2. Import Source Hubs as Managed Hubs

Import each source hub cluster into the Global Hub as a managed hub:

> **Important**: The label `global-hub.open-cluster-management.io/deploy-mode: hosted` is **required** for migration to work.

```bash
# Verify source hub is imported and Global Hub agent is running
oc get managedcluster <source-hub-name>
oc get pods -n <agent-namespace> -l name=multicluster-global-hub-agent
```

For detailed import instructions, see the [ACM Import Documentation](https://docs.redhat.com/en/documentation/red_hat_advanced_cluster_management_for_kubernetes/2.15/html/clusters/cluster_mce_overview#import-intro).

### 3. Verify Multi-Hub Setup

Ensure all hubs are properly connected:

```bash
# List all managed hubs
oc get managedcluster -l global-hub.open-cluster-management.io/deploy-mode=hosted

# Check Global Hub agent status on each hub
oc get deployment multicluster-global-hub-agent -n <agent-namespace>
```

Once Global Hub is installed and all participating hubs are imported, you can proceed with cluster redistribution.

---

## Use Cases for Cluster Redistribution

### 1. Capacity Redistribution

**Scenario**: Hub A is approaching resource limits while Hub B has spare capacity.

**Solution**: Migrate a portion of clusters from Hub A to Hub B:

```yaml
apiVersion: global-hub.open-cluster-management.io/v1alpha1
kind: ManagedClusterMigration
metadata:
  name: capacity-rebalance
spec:
  from: hub-a
  to: hub-b
  includedManagedClustersPlacementRef: high-resource-clusters
```

Use Placement to select clusters based on resource consumption labels:

```yaml
apiVersion: cluster.open-cluster-management.io/v1beta1
kind: Placement
metadata:
  name: high-resource-clusters
spec:
  numberOfClusters: 50
  predicates:
  - requiredClusterSelector:
      labelSelector:
        matchExpressions:
          - key: resource-tier
            operator: In
            values: ["high"]
```

### 2. Geographic Load Balancing

**Scenario**: Latency issues between a hub and its managed clusters in distant regions.

**Solution**: Deploy regional hubs and migrate clusters to their nearest hub:

```yaml
# Migrate Asia-Pacific clusters to regional hub
apiVersion: global-hub.open-cluster-management.io/v1alpha1
kind: ManagedClusterMigration
metadata:
  name: apac-regional-migration
spec:
  from: global-hub
  to: apac-hub
  includedManagedClustersPlacementRef: apac-clusters
```

```yaml
apiVersion: cluster.open-cluster-management.io/v1beta1
kind: Placement
metadata:
  name: apac-clusters
spec:
  predicates:
  - requiredClusterSelector:
      labelSelector:
        matchExpressions:
          - key: region
            operator: In
            values: ["asia-pacific", "apac", "ap-southeast"]
```

### 3. Workload-Based Distribution

**Scenario**: Different cluster types require different hub configurations.

**Solution**: Dedicate hubs to specific workload types:

```yaml
# Migrate production clusters to dedicated hub
apiVersion: global-hub.open-cluster-management.io/v1alpha1
kind: ManagedClusterMigration
metadata:
  name: production-cluster-migration
spec:
  from: general-hub
  to: production-hub
  includedManagedClustersPlacementRef: production-clusters
```

```yaml
apiVersion: cluster.open-cluster-management.io/v1beta1
kind: Placement
metadata:
  name: production-clusters
spec:
  predicates:
  - requiredClusterSelector:
      labelSelector:
        matchLabels:
          environment: production
```

### 4. Hub Consolidation

**Scenario**: Multiple underutilized hubs consuming unnecessary resources.

**Solution**: Consolidate clusters onto fewer, well-utilized hubs:

```yaml
# Migrate all clusters from hub-c to hub-a
apiVersion: global-hub.open-cluster-management.io/v1alpha1
kind: ManagedClusterMigration
metadata:
  name: hub-consolidation
spec:
  from: hub-c
  to: hub-a
  includedManagedClustersPlacementRef: all-hub-c-clusters
```

## Best Practices for Load Balancing

### 1. Monitor Hub Health Metrics

Before migration, assess hub capacity:

```bash
# Check resource utilization on hub cluster
oc top nodes
oc top pods -n open-cluster-management

# Check API server latency
oc get --raw /metrics | grep apiserver_request_duration
```

### 2. Use Gradual Migration

Avoid overwhelming target hubs by migrating in batches:

```yaml
spec:
  numberOfClusters: 50  # Migrate 50 at a time
```

### 3. Label Clusters for Easy Selection

Implement a consistent labeling strategy:

```yaml
metadata:
  labels:
    region: us-east
    environment: production
    resource-tier: high
    migration-priority: "1"
```

### 4. Validate After Each Batch

Ensure target hub stability before proceeding:

```bash
# Check cluster availability
oc get managedcluster | grep -c "True.*True"

# Verify policy compliance
oc get policy -A | grep -c "Compliant"

# Monitor hub resource usage
oc top nodes
```

## Performance Considerations

Our testing with 300 clusters demonstrates:

| Metric | Performance |
|--------|-------------|
| Migration throughput | ~0.93 seconds per cluster |
| Policy convergence | <2 minutes post-migration |
| Maximum batch size tested | 300 clusters |
| Rollback time | <5 minutes for 300 clusters |

### Optimization Tips

1. **Configure appropriate timeouts**:
   ```yaml
   spec:
     supportedConfigs:
       stageTimeout: 15m  # Increase for larger batches
   ```

2. **Ensure sufficient hub resources**: Target hubs should have headroom for incoming clusters

3. **Pre-stage policy applications**: Apply policies to target hub before migration

## Handling Migration Failures

If a migration batch fails, Global Hub provides automatic rollback:

- Clusters remain operational on source hub
- Partial migrations are reversed
- System returns to known good state

For manual intervention, use the rollback annotation:

```bash
oc annotate managedclustermigration capacity-rebalance \
  global-hub.open-cluster-management.io/migration-request=rollback
```

## Example: Complete Load Balancing Workflow

### Initial State
- Hub A: 200 clusters (80% capacity)
- Hub B: 50 clusters (20% capacity)

### Goal
- Hub A: 125 clusters (50% capacity)
- Hub B: 125 clusters (50% capacity)

### Execution

```yaml
# Step 1: Create placement for migration candidates
apiVersion: cluster.open-cluster-management.io/v1beta1
kind: Placement
metadata:
  name: hub-a-rebalance
spec:
  numberOfClusters: 75
  predicates:
  - requiredClusterSelector:
      labelSelector:
        matchExpressions:
          - key: priority
            operator: NotIn
            values: ["critical"]
---
# Step 2: Execute migration
apiVersion: global-hub.open-cluster-management.io/v1alpha1
kind: ManagedClusterMigration
metadata:
  name: hub-a-to-b-rebalance
spec:
  from: hub-a
  to: hub-b
  includedManagedClustersPlacementRef: hub-a-rebalance
```

### Result
- Hub A: 125 clusters (50% capacity)
- Hub B: 125 clusters (50% capacity)
- Zero downtime during migration
- All policies remain compliant

## Summary

Managed Cluster Migration enables dynamic control plane optimization:

- **Balance load** across multiple hub clusters
- **Scale horizontally** without disruption
- **Optimize resources** through consolidation
- **Reduce latency** with regional distribution

For large-scale Kubernetes deployments, this capability transforms static hub architectures into dynamic, responsive infrastructure that adapts to changing needs.

---

## Getting Started

To explore Managed Cluster Migration:

1. Review the [Global Hub Cluster Migration Guide](https://github.com/ldpliu/multicluster-global-hub/blob/main/doc/migration/global_hub_cluster_migration.md)
2. For ZTP clusters, see [ClusterInstance ZTP Migration Guide](https://github.com/ldpliu/multicluster-global-hub/blob/main/doc/migration/clusterinstance_ztp_migration.md)
3. Check [Migration Performance](https://github.com/ldpliu/multicluster-global-hub/blob/main/doc/migration/migration_performance.md) for scale testing results
