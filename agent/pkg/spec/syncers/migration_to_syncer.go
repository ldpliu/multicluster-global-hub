// Copyright (c) 2024 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package syncers

import (
	"context"
	"encoding/json"
	"fmt"

	addonv1 "github.com/stolostron/klusterlet-addon-controller/pkg/apis/agent/v1"
	"go.uber.org/zap"
	rbacv1 "k8s.io/api/rbac/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	operatorv1 "open-cluster-management.io/api/operator/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stolostron/multicluster-global-hub/agent/pkg/configs"
	migrationv1alpha1 "github.com/stolostron/multicluster-global-hub/operator/api/migration/v1alpha1"
	"github.com/stolostron/multicluster-global-hub/pkg/bundle/migration"
	eventversion "github.com/stolostron/multicluster-global-hub/pkg/bundle/version"
	"github.com/stolostron/multicluster-global-hub/pkg/constants"
	"github.com/stolostron/multicluster-global-hub/pkg/logger"
	"github.com/stolostron/multicluster-global-hub/pkg/transport"
)

type managedClusterMigrationToSyncer struct {
	log             *zap.SugaredLogger
	client          client.Client
	transportClient transport.TransportClient
	bundleVersion   *eventversion.Version
}

func NewManagedClusterMigrationToSyncer(client client.Client,
	transportClient transport.TransportClient,
) *managedClusterMigrationToSyncer {
	return &managedClusterMigrationToSyncer{
		log:             logger.DefaultZapLogger(),
		client:          client,
		transportClient: transportClient,
		bundleVersion:   eventversion.NewVersion(),
	}
}

func (s *managedClusterMigrationToSyncer) Sync(ctx context.Context, payload []byte) error {
	// handle migration.to cloud event
	s.log.Info("received migration event from global hub")
	managedClusterMigrationToEvent := &migration.ManagedClusterMigrationToEvent{}
	if err := json.Unmarshal(payload, managedClusterMigrationToEvent); err != nil {
		return fmt.Errorf("failed to unmarshal payload %v", err)
	}
	s.log.Debugf("received cloudevent %s", string(payload))
	msaName := managedClusterMigrationToEvent.ManagedServiceAccountName
	msaNamespace := managedClusterMigrationToEvent.ManagedServiceAccountInstallNamespace

	if managedClusterMigrationToEvent.Stage == migrationv1alpha1.MigrationResourceCleaned {
		// delete the subjectaccessreviews creation role and roleBinding
		migrationClusterRoleName := getMigrationClusterRoleName(msaName)
		clusterRole := &rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{Name: migrationClusterRoleName},
		}
		if err := s.client.Get(ctx,
			client.ObjectKeyFromObject(clusterRole), clusterRole); err != nil {
			if !apierrors.IsNotFound(err) {
				return err
			}
		} else {
			if err = s.client.Delete(ctx, clusterRole); err != nil {
				return err
			}
		}

		sarMigrationClusterRoleBindingName := fmt.Sprintf("%s-subjectaccessreviews-clusterrolebinding", msaName)
		clusterRoleBinding := &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: sarMigrationClusterRoleBindingName,
			},
		}
		if err := s.client.Get(ctx,
			client.ObjectKeyFromObject(clusterRoleBinding), clusterRoleBinding); err != nil {
			if !apierrors.IsNotFound(err) {
				return err
			}
		} else {
			if err = s.client.Delete(ctx, clusterRoleBinding); err != nil {
				return err
			}
		}
		return nil
	}

	if err := s.ensureClusterManager(ctx, msaName, msaNamespace); err != nil {
		return err
	}

	if err := s.ensureMigrationClusterRole(ctx, msaName); err != nil {
		return err
	}

	if err := s.ensureRegistrationClusterRoleBinding(ctx, msaName, msaNamespace); err != nil {
		return err
	}

	if err := s.ensureSARClusterRoleBinding(ctx, msaName, msaNamespace); err != nil {
		return err
	}

	klusterletAddonConfig := managedClusterMigrationToEvent.KlusterletAddonConfig
	existingAddonConfig := &addonv1.KlusterletAddonConfig{}
	if klusterletAddonConfig != nil {
		err := s.client.Get(ctx, client.ObjectKeyFromObject(klusterletAddonConfig), existingAddonConfig)
		if err != nil && !apierrors.IsNotFound(err) {
			s.log.Errorf("failed to get the klusterletAddonConfig %v", err)
			return err
		} else if apierrors.IsNotFound(err) {
			s.log.Infof("deploying migration addonConfigs %s", klusterletAddonConfig.GetName())
			if err := s.client.Create(ctx, klusterletAddonConfig); err != nil {
				s.log.Errorf("cannot create klusterletAddonConfig %v", err)
				return err
			}
		}

		if !apiequality.Semantic.DeepDerivative(existingAddonConfig.Spec, klusterletAddonConfig.Spec) {
			s.log.Infof("updating migration addonConfigs %s", klusterletAddonConfig.GetName())
			existingAddonConfig.Spec = klusterletAddonConfig.Spec
			if err := s.client.Update(ctx, existingAddonConfig); err != nil {
				s.log.Errorf("cannot update klusterletAddonConfig %v", err)
				return err
			}
		}

		// If it's directly sent to the global hub, mark it as completed.
		s.log.Infof("sending addonConfigs applied confirmation %s", klusterletAddonConfig.Name)
		err = SendMigrationEvent(ctx, s.transportClient, configs.GetLeafHubName(), constants.CloudEventGlobalHubClusterName,
			&migration.ManagedClusterMigrationBundle{
				Stage:           migrationv1alpha1.MigrationResourceDeployed,
				ManagedClusters: []string{klusterletAddonConfig.Name},
			},
			s.bundleVersion)
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *managedClusterMigrationToSyncer) ensureClusterManager(ctx context.Context,
	msaName, msaNamespace string,
) error {
	foundClusterManager := &operatorv1.ClusterManager{}
	if err := s.client.Get(ctx,
		types.NamespacedName{Name: "cluster-manager"}, foundClusterManager); err != nil {
		return err
	}

	clusterManager := foundClusterManager.DeepCopy()
	// check if the ManagedClusterAutoApproval feature is enabled and
	// the service account is added to the auto-approve list
	autoApproveUser := fmt.Sprintf("system:serviceaccount:%s:%s", msaNamespace, msaName)
	autoApproveFeatureEnabled := false
	autoApproveUserAdded := false
	clusterManagerChanged := false
	if clusterManager.Spec.RegistrationConfiguration != nil {
		registrationConfiguration := clusterManager.Spec.RegistrationConfiguration
		for i, featureGate := range registrationConfiguration.FeatureGates {
			if featureGate.Feature == "ManagedClusterAutoApproval" {
				autoApproveFeatureEnabled = true
				if featureGate.Mode == operatorv1.FeatureGateModeTypeEnable {
					break
				} else {
					registrationConfiguration.FeatureGates[i].Mode = operatorv1.FeatureGateModeTypeEnable
					clusterManagerChanged = true
					break
				}
			}
		}
		if !autoApproveFeatureEnabled {
			registrationConfiguration.FeatureGates = append(
				registrationConfiguration.FeatureGates,
				operatorv1.FeatureGate{
					Feature: "ManagedClusterAutoApproval",
					Mode:    operatorv1.FeatureGateModeTypeEnable,
				})
			clusterManagerChanged = true
		}
		for _, user := range registrationConfiguration.AutoApproveUsers {
			if user == autoApproveUser {
				autoApproveUserAdded = true
				break
			}
		}
		if !autoApproveUserAdded {
			registrationConfiguration.AutoApproveUsers = append(
				registrationConfiguration.AutoApproveUsers,
				autoApproveUser)
			clusterManagerChanged = true
		}
	} else {
		clusterManager.Spec.RegistrationConfiguration = &operatorv1.RegistrationHubConfiguration{
			FeatureGates: []operatorv1.FeatureGate{
				{
					Feature: "ManagedClusterAutoApproval",
					Mode:    operatorv1.FeatureGateModeTypeEnable,
				},
			},
			AutoApproveUsers: []string{
				autoApproveUser,
			},
		}
		clusterManagerChanged = true
	}

	// patch cluster-manager only if it has changed
	if clusterManagerChanged {
		if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			if err := s.client.Update(ctx, clusterManager); err != nil {
				return err
			}
			return nil
		}); err != nil {
			s.log.Errorw("failed to update clusterManager", "error", err)
		}
	}

	return nil
}

func (s *managedClusterMigrationToSyncer) ensureMigrationClusterRole(ctx context.Context, msaName string) error {
	// create or update clusterrole for the migration service account
	migrationClusterRoleName := getMigrationClusterRoleName(msaName)
	migrationClusterRole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: migrationClusterRoleName,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"authorization.k8s.io"},
				Resources: []string{"subjectaccessreviews"},
				Verbs:     []string{"create"},
			},
		},
	}

	foundMigrationClusterRole := &rbacv1.ClusterRole{}
	if err := s.client.Get(ctx,
		types.NamespacedName{
			Name: migrationClusterRoleName,
		}, foundMigrationClusterRole); err != nil {
		if apierrors.IsNotFound(err) {
			s.log.Infof("creating migration clusterrole %s", foundMigrationClusterRole.GetName())
			s.log.Debugf("creating migration clusterrole %v", foundMigrationClusterRole)
			if err := s.client.Create(ctx, migrationClusterRole); err != nil {
				return err
			}
		} else {
			return err
		}
	} else {
		if !apiequality.Semantic.DeepDerivative(migrationClusterRole, foundMigrationClusterRole) {
			s.log.Infof("updating migration clusterrole %s", migrationClusterRole.GetName())
			s.log.Debugf("updating migration clusterrole %v", migrationClusterRole)
			if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
				if err := s.client.Update(ctx, migrationClusterRole); err != nil {
					return err
				}
				return nil
			}); err != nil {
				s.log.Errorw("failed to update migration ClusterRole", "error", err)
			}
		}
	}

	return nil
}

func (s *managedClusterMigrationToSyncer) ensureRegistrationClusterRoleBinding(ctx context.Context,
	msaName, msaNamespace string,
) error {
	registrationClusterRoleName := "open-cluster-management:managedcluster:bootstrap:agent-registration"
	registrationClusterRoleBindingName := fmt.Sprintf("agent-registration-clusterrolebinding:%s", msaName)
	registrationClusterRoleBinding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: registrationClusterRoleBindingName,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      msaName,
				Namespace: msaNamespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			Kind:     "ClusterRole",
			Name:     registrationClusterRoleName,
			APIGroup: "rbac.authorization.k8s.io",
		},
	}

	foundRegistrationClusterRoleBinding := &rbacv1.ClusterRoleBinding{}
	if err := s.client.Get(ctx,
		types.NamespacedName{
			Name: registrationClusterRoleBindingName,
		}, foundRegistrationClusterRoleBinding); err != nil {
		if apierrors.IsNotFound(err) {
			s.log.Info("creating agent registration clusterrolebinding",
				"clusterrolebinding", registrationClusterRoleBindingName)
			if err := s.client.Create(ctx, registrationClusterRoleBinding); err != nil {
				return err
			}
		} else {
			return err
		}
	} else {
		if !apiequality.Semantic.DeepDerivative(registrationClusterRoleBinding, foundRegistrationClusterRoleBinding) {
			s.log.Info("updating agent registration clusterrolebinding",
				"clusterrolebinding", registrationClusterRoleBindingName)
			if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
				if err := s.client.Update(ctx, registrationClusterRoleBinding); err != nil {
					return err
				}
				return nil
			}); err != nil {
				s.log.Errorw("failed to update migration ClusterRoleBinding", "error", err)
			}
		}
	}

	return nil
}

func (s *managedClusterMigrationToSyncer) ensureSARClusterRoleBinding(ctx context.Context,
	msaName, msaNamespace string,
) error {
	migrationClusterRoleName := getMigrationClusterRoleName(msaName)
	sarMigrationClusterRoleBindingName := fmt.Sprintf("%s-subjectaccessreviews-clusterrolebinding", msaName)
	sarMigrationClusterRoleBinding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: sarMigrationClusterRoleBindingName,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      msaName,
				Namespace: msaNamespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			Kind:     "ClusterRole",
			Name:     migrationClusterRoleName,
			APIGroup: "rbac.authorization.k8s.io",
		},
	}

	foundSAMigrationClusterRoleBinding := &rbacv1.ClusterRoleBinding{}
	if err := s.client.Get(ctx,
		types.NamespacedName{
			Name: sarMigrationClusterRoleBindingName,
		}, foundSAMigrationClusterRoleBinding); err != nil {
		if apierrors.IsNotFound(err) {
			s.log.Infof("creating subjectaccessreviews clusterrolebinding %s",
				foundSAMigrationClusterRoleBinding.GetName())
			s.log.Debugf("creating subjectaccessreviews clusterrolebinding %v",
				foundSAMigrationClusterRoleBinding)
			if err := s.client.Create(ctx, sarMigrationClusterRoleBinding); err != nil {
				return err
			}
		} else {
			return err
		}
	} else {
		if !apiequality.Semantic.DeepDerivative(sarMigrationClusterRoleBinding, foundSAMigrationClusterRoleBinding) {
			s.log.Infof("updating subjectaccessreviews clusterrolebinding %v",
				sarMigrationClusterRoleBinding.GetName())
			s.log.Debugf("updating subjectaccessreviews clusterrolebinding %v",
				sarMigrationClusterRoleBinding)
			if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
				if err := s.client.Update(ctx, sarMigrationClusterRoleBinding); err != nil {
					return err
				}
				return nil
			}); err != nil {
				s.log.Errorw("failed to update subjectaccessreviews ClusterRoleBinding", "error", err)
			}

		}
	}

	return nil
}

func getMigrationClusterRoleName(managedServiceAccountName string) string {
	return fmt.Sprintf("multicluster-global-hub-migration:%s", managedServiceAccountName)
}
