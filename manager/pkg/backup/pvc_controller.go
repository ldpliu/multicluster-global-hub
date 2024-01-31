/*
Copyright 2023.

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

package backup

import (
	"context"
	"reflect"
	"time"

	"gorm.io/gorm"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/stolostron/multicluster-global-hub/pkg/constants"
	"github.com/stolostron/multicluster-global-hub/pkg/database"
	"github.com/stolostron/multicluster-global-hub/pkg/utils"
)

var (
	backupLog = ctrl.Log.WithName("backup pvc")
)

// BackupReconciler reconciles a MulticlusterGlobalHub object
type BackupPVCReconciler struct {
	manager.Manager
	client.Client
	gormConn *gorm.DB
}

func NewBackupPVCReconciler(mgr manager.Manager, gormConn *gorm.DB) *BackupPVCReconciler {
	return &BackupPVCReconciler{
		Manager:  mgr,
		Client:   mgr.GetClient(),
		gormConn: gormConn,
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *BackupPVCReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).Named("backupPvcController").
		For(&corev1.PersistentVolumeClaim{},
			builder.WithPredicates(pvcPred)).
		Complete(r)
}

var pvcPred = predicate.Funcs{
	CreateFunc: func(e event.CreateEvent) bool {
		return false
	},
	UpdateFunc: func(e event.UpdateEvent) bool {
		//Only watch pvcs which need backup
		if !utils.HasLabel(e.ObjectNew.GetLabels(), constants.BackupVolumnKey, constants.BackupGlobalHubValue) {
			return false
		}
		//Only run backup when acm scheduled the backup
		if !utils.HasLabelKey(e.ObjectNew.GetLabels(), constants.BackupPvcLastSchedule) {
			return false
		}

		if reflect.DeepEqual(e.ObjectNew.GetLabels()[constants.BackupPvcLastSchedule],
			e.ObjectOld.GetLabels()[constants.BackupPvcLastSchedule]) {
			return false
		}

		return true
	},
	DeleteFunc: func(e event.DeleteEvent) bool {
		return false
	},
}

func (r *BackupPVCReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	isBackupEnabled, err := utils.IsBackupEnabled(ctx, r.Client)

	if err != nil {
		backupLog.Error(err, "failed to get backup enabled", "req", req)
		return ctrl.Result{}, err
	}
	database.IsBackupEnabled = isBackupEnabled
	if !isBackupEnabled {
		backupLog.V(2).Info("Backup is not enabled")
		return ctrl.Result{}, nil
	}

	backupLog.Info("Start backup pvc", "req", req)
	pvc := &corev1.PersistentVolumeClaim{}
	err = r.Client.Get(ctx, req.NamespacedName, pvc)
	if err != nil {
		return ctrl.Result{}, err
	}

	err = database.Lock(r.gormConn)
	if err != nil {
		backupLog.Error(err, "failed to get db lock")
		return ctrl.Result{}, err
	}

	defer database.Unlock(r.gormConn)
	formatTriggerTime := pvc.GetLabels()[constants.BackupPvcLastSchedule]

	err = utils.AddLabel(ctx, r.Client, pvc, pvc.Namespace, pvc.Name, constants.BackupPvcHook, formatTriggerTime)
	if err != nil {
		backupLog.Error(err, "failed to add pvc hook label")
		return ctrl.Result{}, err
	}

	if err := wait.PollImmediate(5*time.Second, 10*time.Minute, func() (bool, error) {
		pvc := &corev1.PersistentVolumeClaim{}
		err := r.Client.Get(ctx, req.NamespacedName, pvc)

		if err != nil {
			return false, nil
		}
		if !utils.HasLabel(pvc.Labels, constants.BackupPvcLastHookName, formatTriggerTime) {
			return false, nil
		}

		return true, nil
	}); err != nil {
		backupLog.Error(err, "Time out to wait backup pvc finished")
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}
