package transporter

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/stolostron/multicluster-global-hub/operator/api/operator/v1alpha4"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/config"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/controllers/transporter/protocol"
	operatorutils "github.com/stolostron/multicluster-global-hub/operator/pkg/utils"
	"github.com/stolostron/multicluster-global-hub/pkg/constants"
	"github.com/stolostron/multicluster-global-hub/pkg/transport"
	"github.com/stolostron/multicluster-global-hub/pkg/utils"
)

type TransportReconciler struct {
	ctrl.Manager
	kafkaController *protocol.KafkaController
	transporter     transport.Transporter
}

var WatchedSecret = sets.NewString(
	constants.GHTransportSecretName,
)

func StartController(initOption config.InitOption) (bool, error) {
	err := NewTransportReconciler(initOption.Mgr).SetupWithManager(initOption.Mgr)
	if err != nil {
		return false, err
	}
	klog.Infof("inited controller: %v", initOption.ControllerName)
	return true, nil
}

func NewTransportReconciler(mgr ctrl.Manager) *TransportReconciler {
	return &TransportReconciler{Manager: mgr}
}

// SetupWithManager sets up the controller with the Manager.
func (r *TransportReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).Named("transportController").
		For(&v1alpha4.MulticlusterGlobalHub{},
			builder.WithPredicates(mghPred)).
		Watches(&corev1.Secret{},
			&handler.EnqueueRequestForObject{}, builder.WithPredicates(secretPred)).
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

var secretPred = predicate.Funcs{
	CreateFunc: func(e event.CreateEvent) bool {
		return secretCond(e.Object)
	},
	UpdateFunc: func(e event.UpdateEvent) bool {
		return secretCond(e.ObjectNew)
	},
	DeleteFunc: func(e event.DeleteEvent) bool {
		return secretCond(e.Object)
	},
}

func secretCond(obj client.Object) bool {
	if WatchedSecret.Has(obj.GetName()) {
		return true
	}
	if obj.GetLabels()["strimzi.io/cluster"] == protocol.KafkaClusterName &&
		obj.GetLabels()["strimzi.io/kind"] == "KafkaUser" {
		return true
	}
	return false
}

// Resources reconcile the transport resources and also update transporter on the configuration
func (r *TransportReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	mgh, err := config.GetMulticlusterGlobalHub(ctx, r.GetClient())
	if err != nil {
		return ctrl.Result{}, err
	}
	if mgh == nil || mgh.DeletionTimestamp != nil {
		return ctrl.Result{}, nil
	}
	var reconcileErr error
	defer func() {
		if !config.IsBYOKafka() {
			return
		}
		config.UpdateMghComponentStatus(ctx, reconcileErr, r.GetClient(),
			mgh, config.COMPONENTS_KAFKA_NAME,
			r.isTransportReady,
		)
	}()

	kafkaSecret := &corev1.Secret{}
	err = r.GetClient().Get(ctx, types.NamespacedName{
		Name:      constants.GHTransportSecretName,
		Namespace: utils.GetDefaultNamespace(),
	}, kafkaSecret)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		config.SetBYOKafka(false)
		config.SetTransporterProtocol(transport.StrimziTransporter)
	} else {
		config.SetBYOKafka(true)
		config.SetTransporterProtocol(transport.SecretTransporter)
	}
	klog.Infof("Use BYO kafka: %v", config.IsBYOKafka())
	klog.Infof("Transport type: %v", config.TransporterProtocol())

	// set the transporter
	switch config.TransporterProtocol() {
	case transport.StrimziTransporter:
		r.transporter = protocol.NewStrimziTransporter(
			r.Manager,
			mgh,
			protocol.WithContext(ctx),
			protocol.WithCommunity(operatorutils.IsCommunityMode()),
		)

		// this controller also will update the transport connection
		if r.kafkaController == nil {
			r.kafkaController, err = protocol.StartKafkaController(ctx, r.Manager, r.transporter)
			if err != nil {
				return ctrl.Result{}, err
			}
		}
	case transport.SecretTransporter:
		r.transporter = protocol.NewBYOTransporter(ctx, types.NamespacedName{
			Namespace: mgh.Namespace,
			Name:      constants.GHTransportSecretName,
		}, r.GetClient())
		// all of hubs will get the same credential
		conn, err := r.transporter.GetConnCredential("")
		if err != nil {
			return ctrl.Result{}, err
		}
		config.SetTransporterConn(conn)
	}
	return ctrl.Result{}, nil
}

func (r *TransportReconciler) isTransportReady(ctx context.Context, c client.Client, namespace, name string) (config.ComponentStatus, error) {
	if config.GetTransporterConn() == nil {
		return config.ComponentStatus{
			Ready:  false,
			Kind:   "TransportConnection",
			Reason: "TransportConnectionNotSet",
			Msg:    "Transport connection is null",
		}, nil
	}

	return config.ComponentStatus{
		Ready:  true,
		Kind:   "TransportConnection",
		Reason: "TransportConnectionSet",
		Msg:    "Use customized transport, connection has set using provided secret",
	}, nil
}
