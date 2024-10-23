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

package mch

import (
	"context"
	"time"

	mchv1 "github.com/stolostron/multiclusterhub-operator/api/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/stolostron/multicluster-global-hub/operator/api/operator/v1alpha4"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/config"
	"github.com/stolostron/multicluster-global-hub/pkg/utils"
)

var mchCrdName = "multiclusterhubs.operator.open-cluster-management.io"

// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch;update

type MchController struct {
	manager.Manager
}

func (r *MchController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	mch, err := utils.ListMCH(ctx, r.GetClient())
	if err != nil {
		return ctrl.Result{}, err
	}
	if mch == nil {
		klog.Infof("no mch found")
		config.SetACMResourceReady(false)
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}
	if mch.Status.Phase != mchv1.HubRunning {
		config.SetACMResourceReady(false)
		err = config.UpdateCondition(ctx, r.GetClient(), config.GetMGHNamespacedName(),
			metav1.Condition{
				Type:    config.CONDITION_TYPE_ACM_READY,
				Status:  config.CONDITION_STATUS_FALSE,
				Reason:  config.CONDITION_REASON_ACM_NOT_READY,
				Message: config.CONDITION_MESSAGE_ACM_NOT_READY,
			}, v1alpha4.GlobalHubError)
		if err != nil {
			return ctrl.Result{}, err
		}
		klog.Infof("mch is not running, phase: %v", mch.Status.Phase)
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}
	klog.Infof("mch is running")
	config.SetACMResourceReady(true)
	err = config.UpdateCondition(ctx, r.GetClient(), config.GetMGHNamespacedName(),
		metav1.Condition{
			Type:    config.CONDITION_TYPE_ACM_READY,
			Status:  config.CONDITION_STATUS_TRUE,
			Reason:  config.CONDITION_REASON_ACM_READY,
			Message: config.CONDITION_MESSAGE_ACM_READY,
		}, v1alpha4.GlobalHubError)
	if err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func StartController(initOption config.InitOption) (bool, error) {
	err := NewMchController(initOption.Mgr).SetupWithManager(initOption.Mgr)
	if err != nil {
		return false, err
	}
	klog.Infof("inited controller: %v", initOption.ControllerName)
	return true, nil
}

func NewMchController(mgr ctrl.Manager) *MchController {
	return &MchController{Manager: mgr}
}

func (r *MchController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("mchController").
		WatchesMetadata(
			&apiextensionsv1.CustomResourceDefinition{},
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(crdPred),
		).
		Complete(r)
}

var crdPred = predicate.Funcs{
	CreateFunc: func(e event.CreateEvent) bool {
		return e.Object.GetName() == mchCrdName
	},
	UpdateFunc: func(e event.UpdateEvent) bool {
		return e.ObjectNew.GetName() == mchCrdName
	},
	DeleteFunc: func(e event.DeleteEvent) bool {
		return false
	},
}
