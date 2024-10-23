package webhook

import (
	"context"
	"embed"
	"fmt"
	"reflect"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/restmapper"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/stolostron/multicluster-global-hub/operator/api/operator/v1alpha4"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/config"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/deployer"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/renderer"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/utils"
	"github.com/stolostron/multicluster-global-hub/pkg/constants"
)

//go:embed manifests
var fs embed.FS

type WebhookReconciler struct {
	ctrl.Manager
}

func StartController(initOption config.InitOption) (bool, error) {
	if !config.IsACMResourceReady() {
		return false, nil
	}
	err := NewWebhookReconciler(initOption.Mgr).SetupWithManager(initOption.Mgr)
	if err != nil {
		return false, err
	}
	klog.Infof("inited controller: %v", initOption.ControllerName)
	return true, nil
}

func NewWebhookReconciler(mgr ctrl.Manager,
) *WebhookReconciler {
	return &WebhookReconciler{
		Manager: mgr,
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *WebhookReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).Named("webhookController").
		For(&v1alpha4.MulticlusterGlobalHub{},
			builder.WithPredicates(mghPred)).
		Watches(&admissionv1.MutatingWebhookConfiguration{},
			&handler.EnqueueRequestForObject{}, builder.WithPredicates(webhookPred)).
		Complete(r)
}

var webhookPred = predicate.Funcs{
	CreateFunc: func(e event.CreateEvent) bool {
		return false
	},
	UpdateFunc: func(e event.UpdateEvent) bool {
		newConfig := e.ObjectNew.(*admissionv1.MutatingWebhookConfiguration)
		oldConfig := e.ObjectOld.(*admissionv1.MutatingWebhookConfiguration)
		if e.ObjectNew.GetLabels()[constants.GlobalHubOwnerLabelKey] ==
			constants.GHOperatorOwnerLabelVal {
			if len(newConfig.Webhooks) != len(oldConfig.Webhooks) ||
				newConfig.Webhooks[0].Name != oldConfig.Webhooks[0].Name ||
				!reflect.DeepEqual(newConfig.Webhooks[0].AdmissionReviewVersions,
					newConfig.Webhooks[0].AdmissionReviewVersions) ||
				!reflect.DeepEqual(newConfig.Webhooks[0].Rules, oldConfig.Webhooks[0].Rules) ||
				!reflect.DeepEqual(newConfig.Webhooks[0].ClientConfig.Service, oldConfig.Webhooks[0].ClientConfig.Service) {
				return true
			}
			return false
		}
		return false
	},
	DeleteFunc: func(e event.DeleteEvent) bool {
		return e.Object.GetLabels()[constants.GlobalHubOwnerLabelKey] ==
			constants.GHOperatorOwnerLabelVal
	},
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

func (r *WebhookReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	mgh, err := config.GetMulticlusterGlobalHub(ctx, r.GetClient())
	if err != nil {
		return ctrl.Result{}, err
	}
	if mgh == nil || mgh.DeletionTimestamp != nil {
		return ctrl.Result{}, nil
	}

	// create new HoHRenderer and HoHDeployer
	hohRenderer, hohDeployer := renderer.NewHoHRenderer(fs), deployer.NewHoHDeployer(r.GetClient())

	// create discovery client
	dc, err := discovery.NewDiscoveryClientForConfig(r.Manager.GetConfig())
	if err != nil {
		return ctrl.Result{}, err
	}

	// create restmapper for deployer to find GVR
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(dc))

	webhookObjects, err := hohRenderer.Render("manifests", "", func(profile string) (interface{}, error) {
		return WebhookVariables{
			ImportClusterInHosted: config.GetImportClusterInHosted(),
			Namespace:             mgh.Namespace,
		}, nil
	})
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to render webhook objects: %v", err)
	}
	if err = utils.ManipulateGlobalHubObjects(webhookObjects, mgh, hohDeployer, mapper, r.GetScheme()); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to create/update webhook objects: %v", err)
	}
	return ctrl.Result{}, nil
}

type WebhookVariables struct {
	ImportClusterInHosted bool
	Namespace             string
}
