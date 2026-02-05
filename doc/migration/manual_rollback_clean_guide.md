# Manual Migration Rollback Guide

## Overview

The Multicluster Global Hub migration process consists of the following phases:
- **Pending** → **Validating** → **Initializing** → **Deploying** → **Registering** → **Cleaning** → **Completed**

### When Manual Rollback is Needed

- **Validating**: Migration hasn't started. No rollback needed - correct the CR and recreate.
- **Initializing/Deploying/Registering**: Automatic rollback is triggered on failure. If automatic rollback fails due to network issues or unexpected failures, manual intervention is required.
- **Cleaning**: Migration is essentially complete. Only manual cleanup of residual resources may be needed.

---

## Rollback Procedures by Phase

### 1. Initializing Phase Rollback

**Failure Scenario**: Network connectivity issues, ManagedServiceAccount creation failure, or bootstrap secret/KlusterletConfig configuration failure.

#### 1.1 Source Hub

**Tasks:**
1. Delete the bootstrap secret (namespace: `multicluster-engine`)
2. Delete the KlusterletConfig (`migration-<target-hub-name>`)
3. Remove migration annotations from ManagedClusters
4. Remove pause annotations from ZTP resources (ClusterDeployment, BareMetalHost, DataImage)

<details>
<summary>Batch Script</summary>

```bash
#!/bin/bash
# ============================================================
# Initializing Phase Rollback - Source Hub
# ============================================================

# ----- Configuration (modify these values) -----
TARGET_HUB_NAME="<target-hub-name>"                      # Target hub name
SOURCE_HUB_KUBECONFIG="<path-to-source-hub-kubeconfig>"  # Source hub kubeconfig
CLUSTERS=("cluster1" "cluster2" "cluster3")              # List of affected clusters

# ----- Step 1: Delete bootstrap secret -----
kubectl delete secret bootstrap-${TARGET_HUB_NAME} -n multicluster-engine --kubeconfig=${SOURCE_HUB_KUBECONFIG}

# ----- Step 2: Delete KlusterletConfig -----
kubectl delete klusterletconfig migration-${TARGET_HUB_NAME} --kubeconfig=${SOURCE_HUB_KUBECONFIG}

# ----- Step 3 & 4: Clean up each cluster -----
for CLUSTER in "${CLUSTERS[@]}"; do
  echo "Processing cluster: ${CLUSTER}"

  # Remove migration annotations from ManagedCluster
  kubectl annotate managedcluster ${CLUSTER} \
    global-hub.open-cluster-management.io/migrating- \
    agent.open-cluster-management.io/klusterlet-config- \
    open-cluster-management/disable-auto-import- \
    --kubeconfig=${SOURCE_HUB_KUBECONFIG}

  # Remove pause annotation from ClusterDeployment
  kubectl annotate clusterdeployment ${CLUSTER} -n ${CLUSTER} \
    cluster.open-cluster-management.io/paused- \
    --kubeconfig=${SOURCE_HUB_KUBECONFIG} 2>/dev/null || true

  # Remove pause annotation from BareMetalHost resources
  kubectl get baremetalhost -n ${CLUSTER} -o name --kubeconfig=${SOURCE_HUB_KUBECONFIG} 2>/dev/null | while read BMH; do
    kubectl annotate ${BMH} -n ${CLUSTER} baremetalhost.metal3.io/paused- --kubeconfig=${SOURCE_HUB_KUBECONFIG}
  done

  # Remove pause annotation from DataImage resources
  kubectl get dataimage -n ${CLUSTER} -o name --kubeconfig=${SOURCE_HUB_KUBECONFIG} 2>/dev/null | while read DI; do
    kubectl annotate ${DI} -n ${CLUSTER} baremetalhost.metal3.io/paused- --kubeconfig=${SOURCE_HUB_KUBECONFIG}
  done
done

echo "Source Hub rollback completed."
```

</details>

#### 1.2 Target Hub (Global Hub Environment)

**Tasks:**
1. Delete the ManagedServiceAccount
2. Remove auto-approve user from ClusterManager
3. Delete migration RBAC resources (ClusterRole, ClusterRoleBindings)

<details>
<summary>Batch Script</summary>

```bash
#!/bin/bash
# ============================================================
# Initializing Phase Rollback - Target Hub (Global Hub)
# ============================================================

# ----- Configuration (modify these values) -----
MIGRATION_NAME="<migration-name>"                        # ManagedClusterMigration CR name
TARGET_HUB_NAME="<target-hub-name>"                      # Target hub namespace
GLOBAL_HUB_KUBECONFIG="<path-to-global-hub-kubeconfig>"  # Target hub (Global Hub) kubeconfig

# ----- Step 1: Delete ManagedServiceAccount -----
kubectl delete managedserviceaccount ${MIGRATION_NAME} -n ${TARGET_HUB_NAME} --kubeconfig=${GLOBAL_HUB_KUBECONFIG}

# ----- Step 2: Remove auto-approve user from ClusterManager -----
# User format: system:serviceaccount:<target-hub-name>:<migration-name>
MSA_USER="system:serviceaccount:${TARGET_HUB_NAME}:${MIGRATION_NAME}"
# Get current autoApproveUsers, remove the MSA user, and patch back
kubectl get clustermanager cluster-manager -o json --kubeconfig=${GLOBAL_HUB_KUBECONFIG} | \
  jq --arg user "$MSA_USER" '.spec.registrationConfiguration.autoApproveUsers |= map(select(. != $user))' | \
  kubectl apply -f - --kubeconfig=${GLOBAL_HUB_KUBECONFIG}

# ----- Step 3: Delete RBAC resources -----
kubectl delete clusterrole global-hub-migration-${MIGRATION_NAME}-sar --ignore-not-found=true --kubeconfig=${GLOBAL_HUB_KUBECONFIG}
kubectl delete clusterrolebinding global-hub-migration-${MIGRATION_NAME}-sar --ignore-not-found=true --kubeconfig=${GLOBAL_HUB_KUBECONFIG}
kubectl delete clusterrolebinding global-hub-migration-${MIGRATION_NAME}-registration --ignore-not-found=true --kubeconfig=${GLOBAL_HUB_KUBECONFIG}

echo "Target Hub rollback completed."
```

</details>

---

### 2. Deploying Phase Rollback

**Failure Scenario**: Kafka broker network issues, resource application failure on target hub, or deployment confirmation timeout.

#### 2.1 Source Hub


> Same as [Initializing Phase - Source Hub](#11-source-hub). Use the same batch script.

#### 2.2 Target Hub (Global Hub Environment)

**Tasks:**
1. Delete the ManagedServiceAccount
2. Add pause annotations and remove finalizers from ZTP resources
3. Delete ManagedClusters and namespaces
4. Remove auto-approve user from ClusterManager and delete RBAC resources

<details>
<summary>Batch Script</summary>

```bash
#!/bin/bash
# ============================================================
# Deploying Phase Rollback - Target Hub (Global Hub)
# ============================================================

# ----- Configuration (modify these values) -----
MIGRATION_NAME="<migration-name>"                        # ManagedClusterMigration CR name
TARGET_HUB_NAME="<target-hub-name>"                      # Target hub namespace
GLOBAL_HUB_KUBECONFIG="<path-to-global-hub-kubeconfig>"  # Target hub (Global Hub) kubeconfig
CLUSTERS=("cluster1" "cluster2" "cluster3")              # List of affected clusters

# ----- Step 1: Delete ManagedServiceAccount -----
kubectl delete managedserviceaccount ${MIGRATION_NAME} -n ${TARGET_HUB_NAME} --kubeconfig=${GLOBAL_HUB_KUBECONFIG}

# ----- Step 2 & 3: Clean up each cluster -----
for CLUSTER in "${CLUSTERS[@]}"; do
  echo "Processing cluster: ${CLUSTER}"

  # Add pause annotation and remove finalizers from ClusterDeployment
  kubectl annotate clusterdeployment ${CLUSTER} -n ${CLUSTER} \
    cluster.open-cluster-management.io/paused=true --overwrite \
    --kubeconfig=${GLOBAL_HUB_KUBECONFIG} 2>/dev/null || true
  kubectl patch clusterdeployment ${CLUSTER} -n ${CLUSTER} --type json \
    -p='[{"op": "remove", "path": "/metadata/finalizers"}]' \
    --kubeconfig=${GLOBAL_HUB_KUBECONFIG} 2>/dev/null || true

  # Add pause annotation and remove finalizers from BareMetalHost
  kubectl get baremetalhost -n ${CLUSTER} -o name --kubeconfig=${GLOBAL_HUB_KUBECONFIG} 2>/dev/null | while read BMH; do
    kubectl annotate ${BMH} -n ${CLUSTER} baremetalhost.metal3.io/paused=true --overwrite --kubeconfig=${GLOBAL_HUB_KUBECONFIG}
    kubectl patch ${BMH} -n ${CLUSTER} --type json \
      -p='[{"op": "remove", "path": "/metadata/finalizers"}]' --kubeconfig=${GLOBAL_HUB_KUBECONFIG}
  done

  # Add pause annotation and remove finalizers from DataImage
  kubectl get dataimage -n ${CLUSTER} -o name --kubeconfig=${GLOBAL_HUB_KUBECONFIG} 2>/dev/null | while read DI; do
    kubectl annotate ${DI} -n ${CLUSTER} baremetalhost.metal3.io/paused=true --overwrite --kubeconfig=${GLOBAL_HUB_KUBECONFIG}
    kubectl patch ${DI} -n ${CLUSTER} --type json \
      -p='[{"op": "remove", "path": "/metadata/finalizers"}]' --kubeconfig=${GLOBAL_HUB_KUBECONFIG}
  done

  # Remove finalizers from ImageClusterInstall
  kubectl patch imageclusterinstall ${CLUSTER} -n ${CLUSTER} --type json \
    -p='[{"op": "remove", "path": "/metadata/finalizers"}]' \
    --kubeconfig=${GLOBAL_HUB_KUBECONFIG} 2>/dev/null || true

  # Remove finalizers from BMC credentials secrets
  kubectl get baremetalhost -n ${CLUSTER} -o json --kubeconfig=${GLOBAL_HUB_KUBECONFIG} 2>/dev/null | \
    jq -r '.items[].spec.bmc.credentialsName' 2>/dev/null | \
    while read SECRET_NAME; do
      [ -z "$SECRET_NAME" ] && continue
      kubectl patch secret $SECRET_NAME -n ${CLUSTER} --type json \
        -p='[{"op": "remove", "path": "/metadata/finalizers"}]' \
        --kubeconfig=${GLOBAL_HUB_KUBECONFIG} 2>/dev/null || true
    done

  # Delete ManagedCluster and namespace
  kubectl delete managedcluster ${CLUSTER} --ignore-not-found=true --kubeconfig=${GLOBAL_HUB_KUBECONFIG}
  kubectl delete namespace ${CLUSTER} --ignore-not-found=true --kubeconfig=${GLOBAL_HUB_KUBECONFIG}
done

# ----- Step 4: Remove auto-approve user and delete RBAC -----
MSA_USER="system:serviceaccount:${TARGET_HUB_NAME}:${MIGRATION_NAME}"
kubectl get clustermanager cluster-manager -o json --kubeconfig=${GLOBAL_HUB_KUBECONFIG} | \
  jq --arg user "$MSA_USER" '.spec.registrationConfiguration.autoApproveUsers |= map(select(. != $user))' | \
  kubectl apply -f - --kubeconfig=${GLOBAL_HUB_KUBECONFIG}

kubectl delete clusterrole global-hub-migration-${MIGRATION_NAME}-sar --ignore-not-found=true --kubeconfig=${GLOBAL_HUB_KUBECONFIG}
kubectl delete clusterrolebinding global-hub-migration-${MIGRATION_NAME}-sar --ignore-not-found=true --kubeconfig=${GLOBAL_HUB_KUBECONFIG}
kubectl delete clusterrolebinding global-hub-migration-${MIGRATION_NAME}-registration --ignore-not-found=true --kubeconfig=${GLOBAL_HUB_KUBECONFIG}

echo "Target Hub rollback completed."
```

</details>

---

### 3. Registering Phase Rollback

**Failure Scenario**: Network issues preventing cluster re-registration, cluster connection failures to target hub, or ManifestWork application timeout.

#### 3.1 Source Hub

**Tasks:**
1. Delete the bootstrap secret
2. Delete the KlusterletConfig
3. Remove migration annotations from ManagedClusters
4. Remove pause annotations from ZTP resources
5. Delete managed-cluster-lease for each failed cluster
6. Set HubAcceptsClient to true for each failed cluster
7. Wait for clusters to become available

<details>
<summary>Batch Script</summary>

```bash
#!/bin/bash
# ============================================================
# Registering Phase Rollback - Source Hub
# ============================================================

# ----- Configuration (modify these values) -----
TARGET_HUB_NAME="<target-hub-name>"
SOURCE_HUB_KUBECONFIG="<path-to-source-hub-kubeconfig>"
# List of failed clusters to rollback
# Tip: Get from Global Hub ConfigMap: kubectl get configmap <migration-name> -n multicluster-global-hub -o jsonpath='{.data.failure}'
CLUSTERS=("cluster1" "cluster2" "cluster3")

# ----- Step 1: Delete bootstrap secret -----
kubectl delete secret bootstrap-${TARGET_HUB_NAME} -n multicluster-engine --kubeconfig=${SOURCE_HUB_KUBECONFIG}

# ----- Step 2: Delete KlusterletConfig -----
kubectl delete klusterletconfig migration-${TARGET_HUB_NAME} --kubeconfig=${SOURCE_HUB_KUBECONFIG}

# ----- Step 3-6: Process each failed cluster -----
for CLUSTER in "${CLUSTERS[@]}"; do
  echo "Processing cluster: ${CLUSTER}"

  # Remove migration annotations
  kubectl annotate managedcluster ${CLUSTER} \
    global-hub.open-cluster-management.io/migrating- \
    agent.open-cluster-management.io/klusterlet-config- \
    open-cluster-management/disable-auto-import- \
    --kubeconfig=${SOURCE_HUB_KUBECONFIG}

  # Remove pause annotation from ClusterDeployment
  kubectl annotate clusterdeployment ${CLUSTER} -n ${CLUSTER} \
    cluster.open-cluster-management.io/paused- \
    --kubeconfig=${SOURCE_HUB_KUBECONFIG} 2>/dev/null || true

  # Remove pause annotation from BareMetalHost
  kubectl get baremetalhost -n ${CLUSTER} -o name --kubeconfig=${SOURCE_HUB_KUBECONFIG} 2>/dev/null | while read BMH; do
    kubectl annotate ${BMH} -n ${CLUSTER} baremetalhost.metal3.io/paused- --kubeconfig=${SOURCE_HUB_KUBECONFIG}
  done

  # Remove pause annotation from DataImage
  kubectl get dataimage -n ${CLUSTER} -o name --kubeconfig=${SOURCE_HUB_KUBECONFIG} 2>/dev/null | while read DI; do
    kubectl annotate ${DI} -n ${CLUSTER} baremetalhost.metal3.io/paused- --kubeconfig=${SOURCE_HUB_KUBECONFIG}
  done

  # Delete managed-cluster-lease
  kubectl delete lease managed-cluster-lease -n ${CLUSTER} --ignore-not-found=true --kubeconfig=${SOURCE_HUB_KUBECONFIG}

  # Restore HubAcceptsClient to true
  kubectl patch managedcluster ${CLUSTER} --type=json \
    -p='[{"op": "replace", "path": "/spec/hubAcceptsClient", "value": true}]' \
    --kubeconfig=${SOURCE_HUB_KUBECONFIG}
done

# ----- Step 7: Wait for clusters to become available -----
echo "Waiting for clusters to become available..."
TIMEOUT=600
ELAPSED=0
while [ $ELAPSED -lt $TIMEOUT ]; do
  ALL_AVAILABLE=true
  for CLUSTER in "${CLUSTERS[@]}"; do
    STATUS=$(kubectl get managedcluster ${CLUSTER} \
      -o jsonpath='{.status.conditions[?(@.type=="ManagedClusterConditionAvailable")].status}' \
      --kubeconfig=${SOURCE_HUB_KUBECONFIG} 2>/dev/null)
    if [ "$STATUS" != "True" ]; then
      ALL_AVAILABLE=false
      echo "  Cluster ${CLUSTER} not yet available..."
      break
    fi
  done

  if [ "$ALL_AVAILABLE" = true ]; then
    echo "All clusters are available!"
    break
  fi

  sleep 5
  ELAPSED=$((ELAPSED + 5))
done

[ $ELAPSED -ge $TIMEOUT ] && echo "Warning: Timeout waiting for clusters" && exit 1
echo "Source Hub rollback completed."
```

</details>

#### 3.2 Target Hub (Global Hub Environment)
> Same as [Deploying Phase - Target Hub](#22-target-hub-global-hub-environment). Use the same batch script with the failed clusters list.

---

### 4. Cleaning Phase Manual Cleanup

**Scenario**: Migration completed successfully, but the cleaning phase failed to remove residual resources.

#### 4.1 Source Hub

**Tasks:**
1. Delete bootstrap secret and KlusterletConfig
2. Remove finalizers from ZTP resources (ClusterDeployment, BareMetalHost, ImageClusterInstall)
3. Remove finalizers from BMC credentials secrets
4. Delete migrated ManagedClusters

<details>
<summary>Batch Script</summary>

```bash
#!/bin/bash
# ============================================================
# Cleaning Phase - Source Hub
# ============================================================

# ----- Configuration (modify these values) -----
TARGET_HUB_NAME="<target-hub-name>"
SOURCE_HUB_KUBECONFIG="<path-to-source-hub-kubeconfig>"
# List of successfully migrated clusters to clean up
# Tip: Get from Global Hub ConfigMap: kubectl get configmap <migration-name> -n multicluster-global-hub -o jsonpath='{.data.success}'
CLUSTERS=("cluster1" "cluster2" "cluster3")

# ----- Step 1: Delete bootstrap secret and KlusterletConfig -----
kubectl delete secret bootstrap-${TARGET_HUB_NAME} -n multicluster-engine --kubeconfig=${SOURCE_HUB_KUBECONFIG}
kubectl delete klusterletconfig migration-${TARGET_HUB_NAME} --kubeconfig=${SOURCE_HUB_KUBECONFIG}

# ----- Step 2-4: Process each migrated cluster -----
for CLUSTER in "${CLUSTERS[@]}"; do
  echo "Cleaning up cluster: ${CLUSTER}"

  # Delete ObservabilityAddon and remove its finalizers
  kubectl delete observabilityaddon observability-addon -n ${CLUSTER} --ignore-not-found=true --kubeconfig=${SOURCE_HUB_KUBECONFIG}
  kubectl patch observabilityaddon observability-addon -n ${CLUSTER} \
    --type json -p='[{"op": "remove", "path": "/metadata/finalizers"}]' \
    --kubeconfig=${SOURCE_HUB_KUBECONFIG} 2>/dev/null || true

  # Remove finalizers from ClusterDeployment
  kubectl patch clusterdeployment ${CLUSTER} -n ${CLUSTER} --type json \
    -p='[{"op": "remove", "path": "/metadata/finalizers"}]' \
    --kubeconfig=${SOURCE_HUB_KUBECONFIG} 2>/dev/null || true

  # Remove finalizers from BareMetalHost
  kubectl get baremetalhost -n ${CLUSTER} -o name --kubeconfig=${SOURCE_HUB_KUBECONFIG} 2>/dev/null | while read BMH; do
    kubectl patch ${BMH} -n ${CLUSTER} --type json \
      -p='[{"op": "remove", "path": "/metadata/finalizers"}]' \
      --kubeconfig=${SOURCE_HUB_KUBECONFIG} 2>/dev/null || true
  done

  # Remove finalizers from ImageClusterInstall
  kubectl patch imageclusterinstall ${CLUSTER} -n ${CLUSTER} --type json \
    -p='[{"op": "remove", "path": "/metadata/finalizers"}]' \
    --kubeconfig=${SOURCE_HUB_KUBECONFIG} 2>/dev/null || true

  # Remove finalizers from BMC credentials secrets
  kubectl get baremetalhost -n ${CLUSTER} -o json --kubeconfig=${SOURCE_HUB_KUBECONFIG} 2>/dev/null | \
    jq -r '.items[].spec.bmc.credentialsName' 2>/dev/null | \
    while read SECRET_NAME; do
      [ -z "$SECRET_NAME" ] && continue
      kubectl patch secret $SECRET_NAME -n ${CLUSTER} --type json \
        -p='[{"op": "remove", "path": "/metadata/finalizers"}]' \
        --kubeconfig=${SOURCE_HUB_KUBECONFIG} 2>/dev/null || true
    done

  # Delete the ManagedCluster
  kubectl delete managedcluster ${CLUSTER} --kubeconfig=${SOURCE_HUB_KUBECONFIG}
  echo "Deleted cluster: ${CLUSTER}"
done

echo "Source Hub cleanup completed."
```

</details>

#### 4.2 Target Hub (Global Hub Environment)

**Tasks:**
1. Delete the ManagedServiceAccount
2. Remove auto-import disable annotation from migrated clusters
3. Remove velero restore label from ImageClusterInstall
4. Delete migration RBAC resources

<details>
<summary>Batch Script</summary>

```bash
#!/bin/bash
# ============================================================
# Cleaning Phase - Target Hub (Global Hub)
# ============================================================

# ----- Configuration (modify these values) -----
MIGRATION_NAME="<migration-name>"
TARGET_HUB_NAME="<target-hub-name>"
GLOBAL_HUB_KUBECONFIG="<path-to-global-hub-kubeconfig>"
# List of successfully migrated clusters
# Tip: Get from Global Hub ConfigMap: kubectl get configmap <migration-name> -n multicluster-global-hub -o jsonpath='{.data.success}'
CLUSTERS=("cluster1" "cluster2" "cluster3")

# ----- Step 1: Delete ManagedServiceAccount -----
kubectl delete managedserviceaccount ${MIGRATION_NAME} -n ${TARGET_HUB_NAME} --kubeconfig=${GLOBAL_HUB_KUBECONFIG}

# ----- Step 2 & 3: Process each cluster -----
for CLUSTER in "${CLUSTERS[@]}"; do
  echo "Processing cluster: ${CLUSTER}"

  # Remove auto-import disable annotation
  kubectl annotate managedcluster ${CLUSTER} \
    open-cluster-management/disable-auto-import- \
    --kubeconfig=${GLOBAL_HUB_KUBECONFIG}

  # Remove velero restore label from ImageClusterInstall
  kubectl label imageclusterinstall ${CLUSTER} -n ${CLUSTER} \
    velero.io/restore-name- \
    --kubeconfig=${GLOBAL_HUB_KUBECONFIG} 2>/dev/null || true
done

# ----- Step 4: Remove auto-approve user and delete RBAC -----
MSA_USER="system:serviceaccount:${TARGET_HUB_NAME}:${MIGRATION_NAME}"
kubectl get clustermanager cluster-manager -o json --kubeconfig=${GLOBAL_HUB_KUBECONFIG} | \
  jq --arg user "$MSA_USER" '.spec.registrationConfiguration.autoApproveUsers |= map(select(. != $user))' | \
  kubectl apply -f - --kubeconfig=${GLOBAL_HUB_KUBECONFIG}

kubectl delete clusterrole global-hub-migration-${MIGRATION_NAME}-sar --ignore-not-found=true --kubeconfig=${GLOBAL_HUB_KUBECONFIG}
kubectl delete clusterrolebinding global-hub-migration-${MIGRATION_NAME}-sar --ignore-not-found=true --kubeconfig=${GLOBAL_HUB_KUBECONFIG}
kubectl delete clusterrolebinding global-hub-migration-${MIGRATION_NAME}-registration --ignore-not-found=true --kubeconfig=${GLOBAL_HUB_KUBECONFIG}

echo "Target Hub cleanup completed."
```

</details>
