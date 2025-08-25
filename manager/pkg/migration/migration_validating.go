package migration

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	migrationv1alpha1 "github.com/stolostron/multicluster-global-hub/operator/api/migration/v1alpha1"
	"github.com/stolostron/multicluster-global-hub/pkg/constants"
	"github.com/stolostron/multicluster-global-hub/pkg/utils"
)

const (
	ConditionReasonHubClusterInvalid     = "HubClusterInvalid"
	ConditionReasonClusterNotFound       = "ClusterNotFound"
	ConditionReasonClusterConflict       = "ClusterConflict"
	ConditionReasonClusterValidateFailed = "ClusterValidateFailed"

	ConditionReasonResourceValidated = "ResourceValidated"
	ConditionReasonResourceInvalid   = "ResourceInvalid"

	// Managed cluster conditions
	conditionTypeAvailable = "ManagedClusterConditionAvailable"
)

// Only configmap and secret are allowed
var AllowedKinds = map[string]bool{
	"configmap": true,
	"secret":    true,
}

type ManagedClusterInfo struct {
	LeafHubName string
	Annotations map[string]string
	Labels      map[string]string
}

// DNS Subdomain (RFC 1123) — for ConfigMap, Secret, Namespace, etc.
var dns1123SubdomainRegex = regexp.MustCompile(`^[a-z0-9]([-a-z0-9\.]*[a-z0-9])?$`)

// validating performs comprehensive validation of a ManagedClusterMigration request.
// It validates:
// 1. Source and destination hub clusters (existence, availability, and hub capability)
// 2. Managed clusters (existence, availability, and proper hub assignment)
//
// Validation logic:
// - If clusters do not exist in both from and to hubs, report validation error and mark as failed
// - If clusters exist in source hub and are valid, mark as validated and change phase to initializing
// - If clusters exist in destination hub, mark as failed with 'ClusterConflict' message
//
// Returns:
// - bool: true if validation should continue, false if already processed or deleted
// - error: any error encountered during validation
func (m *ClusterMigrationController) validating(ctx context.Context,
	mcm *migrationv1alpha1.ManagedClusterMigration,
) (bool, error) {
	needsRequeue := false
	if mcm.DeletionTimestamp != nil {
		return needsRequeue, nil
	}
	log.Info("migration: %v validating", mcm.Name)

	// reset clusterlist when new migration created
	if mcm.Status.Phase == "" {
		m.currentClusterList = migrationClusterList{}
	}

	if meta.IsStatusConditionTrue(mcm.Status.Conditions, migrationv1alpha1.ConditionTypeValidated) ||
		mcm.Status.Phase != migrationv1alpha1.PhaseValidating {
		return needsRequeue, nil
	}
	log.Info("migration validating")

	condition := metav1.Condition{
		Type:    migrationv1alpha1.ConditionTypeValidated,
		Status:  metav1.ConditionTrue,
		Reason:  ConditionReasonResourceValidated,
		Message: "Migration resources have been validated",
	}

	var err error
	defer func() {
		nextPhase := migrationv1alpha1.PhaseInitializing
		if err != nil {
			condition.Message = err.Error()
			condition.Status = metav1.ConditionFalse
			nextPhase = migrationv1alpha1.PhaseFailed
			if m.EventRecorder != nil {
				m.EventRecorder.Eventf(mcm, corev1.EventTypeWarning, "ValidationFailed", condition.Message)
			}
		}
		if needsRequeue {
			nextPhase = migrationv1alpha1.PhaseValidating
			condition.Status = metav1.ConditionFalse
		}
		err = m.UpdateStatusWithRetry(ctx, mcm, condition, nextPhase)
		if err != nil {
			log.Errorf("failed to update the %s condition: %v", condition.Type, err)
		}
	}()
	log.Info("migration validating from hub")

	// verify fromHub
	if mcm.Spec.From == "" {
		err = fmt.Errorf("source hub is not specified")
		return needsRequeue, err
	}
	if err = validateHubCluster(ctx, m.Client, mcm.Spec.From); err != nil {
		condition.Reason = ConditionReasonHubClusterInvalid
		return needsRequeue, fmt.Errorf("source hub %s: %v", mcm.Spec.From, err)
	}
	log.Info("migration validating to hub")

	// verify toHub
	if mcm.Spec.To == "" {
		err = fmt.Errorf("destination hub is not specified")
		return needsRequeue, err
	}
	if err = validateHubCluster(ctx, m.Client, mcm.Spec.To); err != nil {
		condition.Reason = ConditionReasonHubClusterInvalid
		err = fmt.Errorf("destination hub %s: %v", mcm.Spec.To, err)
		return needsRequeue, err
	}

	log.Info("migration validating clusters")
	if len(mcm.Spec.IncludedManagedClusters) == 0 && mcm.Spec.IncludedManagedClustersPlacementRef == "" {
		err = fmt.Errorf("includedmanagedclusters is empty")
		return needsRequeue, err
	}

	// Get migrate clusters
	clusters, err := m.getMigrationClusters(ctx, mcm)
	if err != nil {
		return needsRequeue, err
	}

	log.Debugf("migrate name:%v, clusters: %v", mcm.Name, clusters)

	// should reconcile when no managedcluster found
	if len(clusters) == 0 {
		condition.Message = "Waiting to get migration clusters from placement"
		needsRequeue = true
		return needsRequeue, nil
	}

	m.currentClusterList = migrationClusterList{
		migrationUID: string(mcm.UID),
		clusterList:  clusters,
	}

	return needsRequeue, nil
}

func (m *ClusterMigrationController) getMigrationClusters(
	ctx context.Context, mcm *migrationv1alpha1.ManagedClusterMigration,
) ([]string, error) {
	migrationUID := string(mcm.UID)

	// If the clusterlist set, do not validate any more
	if m.currentClusterList.migrationUID == migrationUID {
		return m.currentClusterList.clusterList, nil
	}

	// TODO: will load the cluster from configmap first

	// send event to source hub and get placement name from bundle
	if !GetStarted(string(mcm.GetUID()), mcm.Spec.From, migrationv1alpha1.PhaseValidating) {
		var clusterList []string
		if len(mcm.Spec.IncludedManagedClusters) != 0 {
			clusterList = mcm.Spec.IncludedManagedClusters
		}
		err := m.sendEventToSourceHub(ctx, mcm.Spec.From, mcm, migrationv1alpha1.PhaseValidating,
			clusterList, nil, "")
		if err != nil {
			return nil, err
		}
		log.Infof("sent validating events to source hubs: %s", mcm.Spec.From)
		SetStarted(string(mcm.GetUID()), mcm.Spec.From, migrationv1alpha1.PhaseValidating)
	}

	if errMsg := GetErrorMessage(string(mcm.GetUID()), mcm.Spec.From, migrationv1alpha1.PhaseValidating); errMsg != "" {
		return nil, fmt.Errorf("get IncludedManagedClusters from hub %s with err :%s", mcm.Spec.From, errMsg)
	}

	// try to get clusters from MigrationStatusBundle
	migrationClusters := GetClusterList(string(mcm.GetUID()))
	if migrationClusters == nil {
		return nil, nil
	}

	if len(migrationClusters.errList) != 0 {
		for _, ev := range migrationClusters.errList {
			m.EventRecorder.Eventf(mcm, corev1.EventTypeWarning, "ValidationFailed", ev)
		}
		log.Warnf("migration has error list: %v", migrationClusters.errList)
		return nil, fmt.Errorf("%v clusters validate failed, please check the events for details", len(migrationClusters.errList))
	}
	if len(migrationClusters.clusterList) == 0 {
		return nil, nil
	}
	return migrationClusters.clusterList, nil
}

// IsValidResource checks format kind/namespace/name
func IsValidResource(resource string) error {
	parts := strings.Split(resource, "/")
	if len(parts) != 3 {
		return fmt.Errorf("invalid format (must be kind/namespace/name): %s", resource)
	}

	kind, ns, name := strings.ToLower(parts[0]), parts[1], parts[2]

	if !AllowedKinds[kind] {
		return fmt.Errorf("unsupported kind: %s", kind)
	}
	if !dns1123SubdomainRegex.MatchString(ns) {
		return fmt.Errorf("invalid namespace: %s", ns)
	}
	if !dns1123SubdomainRegex.MatchString(name) {
		return fmt.Errorf("invalid name: %s", name)
	}
	return nil
}

// ValidationResult represents the result of validating a single cluster
type ValidationResult struct {
	ErrorMessage    string
	ConditionReason string
}

// validateHubCluster validates if ManagedCluster is a hub cluster and is ready, returns error if not valid
func validateHubCluster(ctx context.Context, c client.Client, name string) error {
	mc := &clusterv1.ManagedCluster{}
	if err := c.Get(ctx, types.NamespacedName{Name: name}, mc); err != nil {
		return err
	}
	// Check cluster Available
	if !isManagedClusterAvailable(mc) {
		return fmt.Errorf("cluster %s is not ready", name)
	}

	// Determine if it is a hub cluster
	if !isHubCluster(ctx, c, mc) {
		return fmt.Errorf("cluster %s is not a hub cluster", name)
	}
	return nil
}

// isManagedClusterAvailable returns true if the ManagedCluster is available (Ready condition is True)
func isManagedClusterAvailable(mc *clusterv1.ManagedCluster) bool {
	for _, cond := range mc.Status.Conditions {
		if cond.Type == conditionTypeAvailable && cond.Status == "True" {
			return true
		}
	}
	return false
}

// isHubCluster determines if ManagedCluster is a hub cluster
func isHubCluster(ctx context.Context, c client.Client, mc *clusterv1.ManagedCluster) bool {
	// Has annotation addon.open-cluster-management.io/on-multicluster-hub=true
	if mc.Annotations != nil && mc.Annotations[constants.AnnotationONMulticlusterHub] == "true" {
		return true
	}
	// local-cluster and has deployment multicluster-global-hub-agent
	if mc.Labels != nil && mc.Labels[constants.LocalClusterName] == "true" {
		// Check agent deployment exists for local-cluster
		agentDeploy := &appsv1.Deployment{}
		err := c.Get(ctx, types.NamespacedName{
			Name:      "multicluster-global-hub-agent",
			Namespace: utils.GetDefaultNamespace(),
		}, agentDeploy)
		if err == nil {
			return true
		}
	}
	return false
}
