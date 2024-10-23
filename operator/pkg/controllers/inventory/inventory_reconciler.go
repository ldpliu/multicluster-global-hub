package inventory

import (
	"context"
	"embed"
	"encoding/base64"
	"fmt"
	"net/url"
	"reflect"

	"github.com/go-logr/logr"
	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/restmapper"
	"k8s.io/klog"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/stolostron/multicluster-global-hub/operator/api/operator/v1alpha4"
	globalhubv1alpha4 "github.com/stolostron/multicluster-global-hub/operator/api/operator/v1alpha4"
	certctrl "github.com/stolostron/multicluster-global-hub/operator/pkg/certificates"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/config"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/deployer"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/renderer"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/utils"
	"github.com/stolostron/multicluster-global-hub/pkg/constants"
)

//go:embed manifests
var fs embed.FS

type InventoryReconciler struct {
	kubeClient kubernetes.Interface
	ctrl.Manager
	log logr.Logger
}

func StartController(initOption config.InitOption) (bool, error) {
	if !config.WithInventory(initOption.Mgh) {
		return false, nil
	}
	if config.GetTransporterConn() == nil {
		return false, nil
	}

	err := NewInventoryReconciler(initOption.Mgr, initOption.KubeClient).SetupWithManager(initOption.Mgr)
	if err != nil {
		return false, err
	}
	klog.Infof("inited controller: %v", initOption.ControllerName)
	return true, nil
}

func NewInventoryReconciler(mgr ctrl.Manager, kubeClient kubernetes.Interface) *InventoryReconciler {
	return &InventoryReconciler{
		log:        ctrl.Log.WithName("global-hub-inventory"),
		Manager:    mgr,
		kubeClient: kubeClient,
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *InventoryReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).Named("inventoryController").
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

func (r *InventoryReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	mgh, err := config.GetMulticlusterGlobalHub(ctx, r.GetClient())
	if err != nil {
		return ctrl.Result{}, err
	}
	if mgh == nil || mgh.DeletionTimestamp != nil {
		return ctrl.Result{}, nil
	}
	var reconcileErr error
	defer func() {
		config.UpdateMghComponentStatus(ctx, reconcileErr, r.GetClient(),
			mgh, config.COMPONENTS_INVENTORY_API_NAME,
			config.IfDeploymentAvailable)
	}()

	// start certificate controller
	certctrl.Start(ctx, r.GetClient(), r.kubeClient)

	// Need to create route so that the cert can use it
	if err := createUpdateInventoryRoute(ctx, r.GetClient(), r.GetScheme(), mgh); err != nil {
		reconcileErr = err
		return ctrl.Result{}, err
	}

	// create inventory certs
	if err := certctrl.CreateInventoryCerts(ctx, r.GetClient(), r.GetScheme(), mgh); err != nil {
		reconcileErr = err
		return ctrl.Result{}, err
	}

	// set the client ca to signing the inventory client cert
	if err := config.SetInventoryClientCA(ctx, mgh.Namespace, certctrl.InventoryClientCASecretName,
		r.GetClient()); err != nil {
		reconcileErr = err
		return ctrl.Result{}, err
	}

	// create new HoHRenderer and HoHDeployer
	hohRenderer, hohDeployer := renderer.NewHoHRenderer(fs), deployer.NewHoHDeployer(r.GetClient())

	// create discovery client
	dc, err := discovery.NewDiscoveryClientForConfig(r.Manager.GetConfig())
	if err != nil {
		reconcileErr = err
		return ctrl.Result{}, err
	}

	// create restmapper for deployer to find GVR
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(dc))

	imagePullPolicy := corev1.PullAlways
	if mgh.Spec.ImagePullPolicy != "" {
		imagePullPolicy = mgh.Spec.ImagePullPolicy
	}

	replicas := int32(1)
	if mgh.Spec.AvailabilityConfig == globalhubv1alpha4.HAHigh {
		replicas = 2
	}

	storageConn := config.GetStorageConnection()
	if storageConn == nil || !config.GetDatabaseReady() {
		reconcileErr = fmt.Errorf("the database isn't ready")
		return ctrl.Result{}, reconcileErr
	}

	postgresURI, err := url.Parse(string(storageConn.SuperuserDatabaseURI))
	if err != nil {
		reconcileErr = err
		return ctrl.Result{}, err
	}
	postgresPassword, ok := postgresURI.User.Password()
	if !ok {
		reconcileErr = fmt.Errorf("failed to get password from database_uri: %s", postgresURI)
		return ctrl.Result{}, reconcileErr
	}

	transportConn := config.GetTransporterConn()
	if transportConn == nil || transportConn.BootstrapServer == "" {
		reconcileErr = fmt.Errorf("the transport connection(%s) must not be empty", transportConn)
		return ctrl.Result{}, reconcileErr
	}

	inventoryObjects, err := hohRenderer.Render("manifests", "", func(profile string) (interface{}, error) {
		return struct {
			Image                string
			Replicas             int32
			ImagePullSecret      string
			ImagePullPolicy      string
			PostgresHost         string
			PostgresPort         string
			PostgresUser         string
			PostgresPassword     string
			PostgresCACert       string
			Namespace            string
			NodeSelector         map[string]string
			Tolerations          []corev1.Toleration
			KafkaBootstrapServer string
			KafkaSSLCAPEM        string
			KafkaSSLCertPEM      string
			KafkaSSLKeyPEM       string
		}{
			Image:                config.GetImage(config.InventoryImageKey),
			Replicas:             replicas,
			ImagePullSecret:      mgh.Spec.ImagePullSecret,
			ImagePullPolicy:      string(imagePullPolicy),
			PostgresHost:         postgresURI.Hostname(),
			PostgresPort:         postgresURI.Port(),
			PostgresUser:         postgresURI.User.Username(),
			PostgresPassword:     postgresPassword,
			PostgresCACert:       base64.StdEncoding.EncodeToString(storageConn.CACert),
			Namespace:            mgh.Namespace,
			NodeSelector:         mgh.Spec.NodeSelector,
			Tolerations:          mgh.Spec.Tolerations,
			KafkaBootstrapServer: transportConn.BootstrapServer,
			KafkaSSLCAPEM:        transportConn.CACert,
			KafkaSSLCertPEM:      transportConn.ClientCert,
			KafkaSSLKeyPEM:       transportConn.ClientKey,
		}, nil
	})
	if err != nil {
		reconcileErr = fmt.Errorf("failed to render inventory objects: %v", err)
		return ctrl.Result{}, reconcileErr
	}
	if err = utils.ManipulateGlobalHubObjects(inventoryObjects, mgh, hohDeployer, mapper, r.GetScheme()); err != nil {
		reconcileErr = fmt.Errorf("failed to create/update inventory objects: %v", err)
		return ctrl.Result{}, reconcileErr
	}
	return ctrl.Result{}, nil
}

func newInventoryRoute(mgh *globalhubv1alpha4.MulticlusterGlobalHub, gvk schema.GroupVersionKind) *routev1.Route {
	return &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      constants.InventoryRouteName,
			Namespace: mgh.Namespace,
			Labels: map[string]string{
				"name": constants.InventoryRouteName,
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         gvk.GroupVersion().String(),
					Kind:               gvk.Kind,
					Name:               mgh.GetName(),
					UID:                mgh.GetUID(),
					BlockOwnerDeletion: ptr.To(true),
					Controller:         ptr.To(true),
				},
			},
		},
		Spec: routev1.RouteSpec{
			Port: &routev1.RoutePort{
				TargetPort: intstr.FromString("http-server"),
			},
			To: routev1.RouteTargetReference{
				Kind: "Service",
				Name: constants.InventoryRouteName,
			},
			TLS: &routev1.TLSConfig{
				Termination:                   routev1.TLSTerminationPassthrough,
				InsecureEdgeTerminationPolicy: routev1.InsecureEdgeTerminationPolicyNone,
			},
		},
	}
}

func createUpdateInventoryRoute(ctx context.Context, c client.Client,
	scheme *runtime.Scheme, mgh *globalhubv1alpha4.MulticlusterGlobalHub,
) error {
	// finds the GroupVersionKind associated with mgh
	gvk, err := apiutil.GVKForObject(mgh, scheme)
	if err != nil {
		return err
	}
	desiredRoute := newInventoryRoute(mgh, gvk)

	existingRoute := &routev1.Route{}
	err = c.Get(ctx, types.NamespacedName{
		Name:      constants.InventoryRouteName,
		Namespace: mgh.Namespace,
	}, existingRoute)
	if err != nil {
		if errors.IsNotFound(err) {
			return c.Create(ctx, desiredRoute)
		}
		return err
	}

	updatedRoute := &routev1.Route{}
	err = utils.MergeObjects(existingRoute, desiredRoute, updatedRoute)
	if err != nil {
		return err
	}

	if !reflect.DeepEqual(updatedRoute.Spec, existingRoute.Spec) {
		return c.Update(ctx, updatedRoute)
	}

	return nil
}
