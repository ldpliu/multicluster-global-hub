/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"fmt"
	"time"

	imagev1client "github.com/openshift/client-go/image/clientset/versioned/typed/image/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/stolostron/multicluster-global-hub/operator/api/operator/v1alpha4"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/config"
	acmaddon "github.com/stolostron/multicluster-global-hub/operator/pkg/controllers/addons"
	agent "github.com/stolostron/multicluster-global-hub/operator/pkg/controllers/agent"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/controllers/agent/addoncontroller"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/controllers/backup"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/controllers/grafana"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/controllers/managedhub"
	globalhubmanager "github.com/stolostron/multicluster-global-hub/operator/pkg/controllers/manager"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/controllers/mch"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/controllers/prune"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/controllers/storage"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/controllers/transporter"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/controllers/webhook"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/utils"
	"github.com/stolostron/multicluster-global-hub/pkg/constants"
	commonutils "github.com/stolostron/multicluster-global-hub/pkg/utils"
)

const (
	globalhubAgentInstaller  = "globalhubAgentInstaller"
	globalhubAgentController = "globalhubAgentController"
	backupController         = "backupController"
	acmAddonsController      = "acmAddonsController"
	transportController      = "transportController"
	managerController        = "managerController"
	grafanaController        = "grafanaController"
	inventoryController      = "inventoryController"
	storageController        = "storageController"
	webhookController        = "webhookController"
	managedClusterController = "managedClusterController"
	mchController            = "mchController"
)

type Func func(initOption config.InitOption) (bool, error)

type InitController struct {
	client            client.Client
	kubeClient        kubernetes.Interface
	imageClient       *imagev1client.ImageV1Client
	mgr               manager.Manager
	initedControllers sets.String
	initFuncMap       map[string]Func
	operatorConfig    *config.OperatorConfig
	upgraded          bool
}

// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch;update
// +kubebuilder:rbac:groups=operator.open-cluster-management.io,resources=multiclusterglobalhubs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator.open-cluster-management.io,resources=multiclusterglobalhubs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=operator.open-cluster-management.io,resources=multiclusterglobalhubs/finalizers,verbs=update
// +kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=managedclustersets/join,verbs=create;delete
// +kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=managedclustersets/bind,verbs=create;delete
// +kubebuilder:rbac:groups=apps.open-cluster-management.io,resources=subscriptions,verbs=get;list;update;patch
// +kubebuilder:rbac:groups=apps.open-cluster-management.io,resources=placementrules,verbs=get;list;update;patch
// +kubebuilder:rbac:groups=apps.open-cluster-management.io,resources=channels,verbs=get;list;update;patch
// +kubebuilder:rbac:groups=policy.open-cluster-management.io,resources=policies,verbs=get;list;patch;update
// +kubebuilder:rbac:groups=policy.open-cluster-management.io,resources=placementbindings,verbs=get;list;patch;update
// +kubebuilder:rbac:groups=app.k8s.io,resources=applications,verbs=get;list;patch;update
// +kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=placements,verbs=create;get;list;patch;update;delete
// +kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=managedclustersetbindings,verbs=create;get;list;patch;update;delete
// +kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=managedclustersets,verbs=get;list;patch;update
// +kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=managedclusters,verbs=get;list;update;create
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;delete;patch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;delete;patch
// +kubebuilder:rbac:groups="apps",resources=deployments,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups="apps",resources=statefulsets,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups="route.openshift.io",resources=routes,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=roles,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=rolebindings,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=clusterroles,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=clusterrolebindings,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups="admissionregistration.k8s.io",resources=mutatingwebhookconfigurations,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=addon.open-cluster-management.io,resources=clustermanagementaddons,verbs=create;delete;get;list;patch;update;watch
// +kubebuilder:rbac:groups=addon.open-cluster-management.io,resources=addondeploymentconfigs,verbs=create;delete;get;list;update;watch
// +kubebuilder:rbac:groups=addon.open-cluster-management.io,resources=clustermanagementaddons/finalizers,verbs=update
// +kubebuilder:rbac:groups=operator.open-cluster-management.io,resources=multiclusterhubs;clustermanagers,verbs=get;list;patch;update;watch
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors;prometheusrules;podmonitors,verbs=get;create;delete;update;list;watch
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=prometheuses/api,resourceNames=k8s,verbs=get;create;update
// +kubebuilder:rbac:groups=operators.coreos.com,resources=subscriptions,verbs=get;create;delete;update;list;watch
// +kubebuilder:rbac:groups=operators.coreos.com,resources=clusterserviceversions,verbs=get;delete;list;watch
// +kubebuilder:rbac:groups=postgres-operator.crunchydata.com,resources=postgresclusters,verbs=get;create;list;watch
// +kubebuilder:rbac:groups=kafka.strimzi.io,resources=kafkas;kafkatopics;kafkausers;kafkanodepools,verbs=get;create;list;watch;update;delete
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch;update
// +kubebuilder:rbac:groups=image.openshift.io,resources=imagestreams,verbs=get;list;watch
// +kubebuilder:rbac:groups="authentication.open-cluster-management.io",resources=managedserviceaccounts,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups="global-hub.open-cluster-management.io",resources=managedclustermigrations,verbs=get;list;watch;update
// +kubebuilder:rbac:groups="config.open-cluster-management.io",resources=klusterletconfigs,verbs=create;delete;get;list;patch;update;watch

func (r *InitController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Check if mgh exist or deleting
	mgh, err := config.GetMulticlusterGlobalHub(ctx, r.client)
	if err != nil || mgh == nil {
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	if config.IsPaused(mgh) {
		klog.Info("mgh controller is paused, nothing more to do")
		return ctrl.Result{}, nil
	}

	initOption := config.InitOption{
		KubeClient:     r.kubeClient,
		Ctx:            ctx,
		OperatorConfig: r.operatorConfig,
		IsMghReady:     meta.IsStatusConditionTrue(mgh.Status.Conditions, config.CONDITION_TYPE_GLOBALHUB_READY),
		Mgr:            r.mgr,
		Mgh:            mgh,
	}

	if mgh.DeletionTimestamp != nil {
		klog.V(2).Info("mgh instance is deleting")
		_, reconcileErr := prune.StartController(initOption)
		if reconcileErr != nil {
			klog.Errorf("failed to init prune controller, err: %v", err)
			return ctrl.Result{}, err
		}
		config.UpdateCondition(ctx, r.client, types.NamespacedName{
			Namespace: mgh.Namespace,
			Name:      mgh.Name,
		}, metav1.Condition{
			Type:    config.CONDITION_TYPE_GLOBALHUB_READY,
			Status:  config.CONDITION_STATUS_FALSE,
			Reason:  config.CONDITION_REASON_GLOBALHUB_UNINSTALL,
			Message: config.CONDITION_MESSAGE_GLOBALHUB_UNINSTALL,
		}, v1alpha4.GlobalHubUninstalling)

		return ctrl.Result{}, nil
	}

	var reconcileErr error
	defer func() {
		if reconcileErr != nil {
			err = config.UpdateCondition(ctx, r.client, types.NamespacedName{
				Namespace: mgh.Namespace,
				Name:      mgh.Name,
			}, metav1.Condition{
				Type:    config.CONDITION_TYPE_GLOBALHUB_READY,
				Status:  config.CONDITION_STATUS_FALSE,
				Reason:  config.CONDITION_REASON_GLOBALHUB_NOT_READY,
				Message: reconcileErr.Error(),
			}, v1alpha4.GlobalHubError)
			if err != nil {
				klog.Errorf("failed to update the instance condition, err: %v", err)
			}
			return
		}

		err = updateMghStatus(ctx, r.client, mgh)
		if err != nil {
			klog.Errorf("failed to update the instance condition, err: %v", err)
		}
	}()

	reconcileErr = config.SetMulticlusterGlobalHubConfig(ctx, mgh, r.client, r.imageClient)
	if reconcileErr != nil {
		return ctrl.Result{}, reconcileErr
	}

	if controllerutil.AddFinalizer(mgh, constants.GlobalHubCleanupFinalizer) {
		if reconcileErr := r.client.Update(ctx, mgh, &client.UpdateOptions{}); reconcileErr != nil {
			if errors.IsConflict(reconcileErr) {
				klog.Errorf("conflict when adding finalizer to mgh instance, error: %v", reconcileErr)
				return ctrl.Result{Requeue: true}, nil
			}
		}
	}

	for controllerName, startController := range r.initFuncMap {
		initOption.ControllerName = controllerName
		if r.initedControllers.Has(controllerName) {
			continue
		}
		var started bool
		started, reconcileErr = startController(initOption)
		if reconcileErr != nil {
			klog.Errorf("failed to init %v, err: %v", controllerName, err)
			return ctrl.Result{}, err
		}
		if started {
			r.initedControllers.Insert(initOption.ControllerName)
		}
	}

	if config.IsACMResourceReady() {
		if config.GetAddonManager() != nil {
			if reconcileErr = utils.TriggerManagedHubAddons(ctx, r.client, config.GetAddonManager()); reconcileErr != nil {
				return ctrl.Result{}, reconcileErr
			}
		}
	}

	return ctrl.Result{}, nil
}

func NewInitController(mgr manager.Manager, kubeClient kubernetes.Interface,
	operatorConfig *config.OperatorConfig, imageClient *imagev1client.ImageV1Client,
) *InitController {
	r := &InitController{
		client:            mgr.GetClient(),
		mgr:               mgr,
		initedControllers: sets.NewString(),
		kubeClient:        kubeClient,
		operatorConfig:    operatorConfig,
		imageClient:       imageClient,
	}
	r.initFuncMap = map[string]Func{
		globalhubAgentInstaller:  agent.StartController,
		globalhubAgentController: addoncontroller.StartController,
		backupController:         backup.StartController,
		acmAddonsController:      acmaddon.StartController,
		transportController:      transporter.StartController,
		managerController:        globalhubmanager.StartController,
		grafanaController:        grafana.StartController,
		storageController:        storage.StartController,
		webhookController:        webhook.StartController,
		managedClusterController: managedhub.StartController,
		mchController:            mch.StartController,
	}
	return r
}

// SetupWithManager sets up the controller with the Manager.
func (r *InitController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).Named("initController").
		For(&v1alpha4.MulticlusterGlobalHub{},
			builder.WithPredicates(mghPred)).
		Complete(r)
}

var mghPred = predicate.Funcs{
	CreateFunc: func(e event.CreateEvent) bool {
		return true
	},
	UpdateFunc: func(e event.UpdateEvent) bool {
		return true
	},
	DeleteFunc: func(e event.DeleteEvent) bool {
		return false
	},
}

func updateMghStatus(ctx context.Context, c client.Client, mgh *v1alpha4.MulticlusterGlobalHub) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		curmgh := &v1alpha4.MulticlusterGlobalHub{}

		err := c.Get(ctx, types.NamespacedName{
			Namespace: mgh.GetNamespace(),
			Name:      mgh.GetName(),
		}, curmgh)
		if err != nil {
			return err
		}

		// update phase
		updatedPhase, desiredPhase := needUpdatePhase(curmgh)

		// update data retention condition
		updatedRetentionCond, desiredConds := updateRetentionConditions(curmgh)

		// update ready condition
		updatedReadyCond, desiredConds := updateReadyConditions(desiredConds, desiredPhase)

		if !updatedPhase && !updatedReadyCond && !updatedRetentionCond {
			return nil
		}

		curmgh.Status.Phase = desiredPhase
		curmgh.Status.Conditions = desiredConds
		err = c.Status().Update(ctx, curmgh)
		return err
	})
}

// dataRetention should at least be 1 month, otherwise it will deleted the current month partitions and records
func updateRetentionConditions(mgh *v1alpha4.MulticlusterGlobalHub) (bool, []metav1.Condition) {
	months, err := commonutils.ParseRetentionMonth(mgh.Spec.DataLayerSpec.Postgres.Retention)
	if err != nil {
		err = fmt.Errorf("failed to parse the retention month, err:%v", err)
		return config.NeedUpdateConditions(mgh.Status.Conditions, metav1.Condition{
			Type:    config.CONDITION_TYPE_DATABASE,
			Status:  config.CONDITION_STATUS_FALSE,
			Reason:  config.CONDITION_REASON_RETENTION_PARSED_FAILED,
			Message: err.Error(),
		})
	}

	if months < 1 {
		months = 1
	}
	msg := fmt.Sprintf("The data will be kept in the database for %d months.", months)
	return config.NeedUpdateConditions(mgh.Status.Conditions, metav1.Condition{
		Type:    config.CONDITION_TYPE_DATABASE,
		Status:  config.CONDITION_STATUS_TRUE,
		Reason:  config.CONDITION_REASON_RETENTION_PARSED,
		Message: msg,
	})
}

func updateReadyConditions(conds []metav1.Condition, phase v1alpha4.GlobalHubPhaseType) (bool, []metav1.Condition) {
	if phase == v1alpha4.GlobalHubRunning {
		return config.NeedUpdateConditions(conds, metav1.Condition{
			Type:    config.CONDITION_TYPE_GLOBALHUB_READY,
			Status:  config.CONDITION_STATUS_TRUE,
			Reason:  config.CONDITION_REASON_GLOBALHUB_READY,
			Message: config.CONDITION_MESSAGE_GLOBALHUB_READY,
		})
	}
	return config.NeedUpdateConditions(conds, metav1.Condition{
		Type:    config.CONDITION_TYPE_GLOBALHUB_READY,
		Status:  config.CONDITION_STATUS_FALSE,
		Reason:  config.CONDITION_REASON_GLOBALHUB_NOT_READY,
		Message: config.CONDITION_MESSAGE_GLOBALHUB_NOT_READY,
	})
}

// needUpdatePhase check if the phase need updated. phase is running only when all the components available
func needUpdatePhase(mgh *v1alpha4.MulticlusterGlobalHub) (bool, v1alpha4.GlobalHubPhaseType) {
	phase := v1alpha4.GlobalHubRunning
	desiredComponents := CheckDesiredComponent(mgh)

	if len(mgh.Status.Components) != desiredComponents.Len() {
		phase = v1alpha4.GlobalHubProgressing
		return phase != mgh.Status.Phase, phase
	}
	for _, dcs := range mgh.Status.Components {
		if !desiredComponents.Has(dcs.Name) {
			phase = v1alpha4.GlobalHubProgressing
		}
		if dcs.Type == config.COMPONENTS_AVAILABLE && dcs.Status != config.CONDITION_STATUS_TRUE {
			phase = v1alpha4.GlobalHubProgressing
		}
	}
	return phase != mgh.Status.Phase, phase
}

func CheckDesiredComponent(mgh *v1alpha4.MulticlusterGlobalHub) sets.String {
	desiredComponents := sets.NewString(
		config.COMPONENTS_MANAGER_NAME,
		config.COMPONENTS_GRAFANA_NAME,
		config.COMPONENTS_POSTGRES_NAME,
		config.COMPONENTS_KAFKA_NAME,
	)
	if config.WithInventory(mgh) {
		desiredComponents.Insert(config.COMPONENTS_INVENTORY_API_NAME)
	}
	return desiredComponents
}
