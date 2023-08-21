package hubofhubs

import (
	"context"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"time"

	globalhubv1alpha4 "github.com/stolostron/multicluster-global-hub/operator/apis/v1alpha4"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/config"
	operatorconstants "github.com/stolostron/multicluster-global-hub/operator/pkg/constants"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/deployer"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/renderer"
	"github.com/stolostron/multicluster-global-hub/pkg/constants"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/restmapper"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/yaml"
)

const (
	datasourceKey     = "datasources.yaml"
	defaultAlertName  = "grafana-alerting-acm-global-default-alerting"
	customAlertName   = "grafana-alerting-acm-global-custom-alerting"
	mergedAlertName   = "grafana-alerting-acm-global-alerting-policy"
	alertConfigMapKey = "acm-global-alerting.yaml"
)

func (r *MulticlusterGlobalHubReconciler) reconcileGrafana(ctx context.Context,
	mgh *globalhubv1alpha4.MulticlusterGlobalHub,
) error {
	log := r.Log.WithName("grafana")

	// generate random session secret for oauth-proxy
	proxySessionSecret, err := config.GetOauthSessionSecret()
	if err != nil {
		return fmt.Errorf("failed to generate random session secret for grafana oauth-proxy: %v", err)
	}

	// generate datasource secret: must before the grafana objects
	datasourceSecretName, err := r.GenerateGrafanaDataSourceSecret(ctx, mgh)
	if err != nil {
		return fmt.Errorf("failed to generate grafana datasource secret: %v", err)
	}

	imagePullPolicy := corev1.PullAlways
	if mgh.Spec.ImagePullPolicy != "" {
		imagePullPolicy = mgh.Spec.ImagePullPolicy
	}

	// get the grafana objects
	grafanaRenderer, grafanaDeployer := renderer.NewHoHRenderer(fs), deployer.NewHoHDeployer(r.Client)
	grafanaObjects, err := grafanaRenderer.Render("manifests/grafana", "", func(profile string) (interface{}, error) {
		return struct {
			Namespace            string
			SessionSecret        string
			ProxyImage           string
			GrafanaImage         string
			ImagePullSecret      string
			ImagePullPolicy      string
			DatasourceSecretName string
			NodeSelector         map[string]string
			Tolerations          []corev1.Toleration
		}{
			Namespace:            config.GetDefaultNamespace(),
			SessionSecret:        proxySessionSecret,
			ProxyImage:           config.GetImage(config.OauthProxyImageKey),
			GrafanaImage:         config.GetImage(config.GrafanaImageKey),
			ImagePullSecret:      mgh.Spec.ImagePullSecret,
			ImagePullPolicy:      string(imagePullPolicy),
			DatasourceSecretName: datasourceSecretName,
			NodeSelector:         mgh.Spec.NodeSelector,
			Tolerations:          mgh.Spec.Tolerations,
		}, nil
	})
	if err != nil {
		return fmt.Errorf("failed to render grafana manifests: %w", err)
	}
	err = r.generateMergedAlertConfigMap(ctx)
	if err != nil {
		return err
	}
	// create restmapper for deployer to find GVR
	dc, err := discovery.NewDiscoveryClientForConfig(r.Manager.GetConfig())
	if err != nil {
		return err
	}
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(dc))

	if err = r.manipulateObj(ctx, grafanaDeployer, mapper, grafanaObjects, mgh, log); err != nil {
		return fmt.Errorf("failed to create/update grafana objects: %w", err)
	}

	log.Info("grafana objects created/updated successfully")
	return nil
}

func (r *MulticlusterGlobalHubReconciler) generateMergedAlertConfigMap(ctx context.Context) error {
	configNamespace := config.GetDefaultNamespace()
	defaultAlertConfigMap, err := r.KubeClient.CoreV1().ConfigMaps(configNamespace).Get(ctx, defaultAlertName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get default alert configmap: %w", err)
	}
	customAlertConfigMap, err := r.KubeClient.CoreV1().ConfigMaps(configNamespace).Get(ctx, customAlertName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			var mergedAlertConfigMap *corev1.ConfigMap
			mergedAlertConfigMap.Name = mergedAlertName
			mergedAlertConfigMap.Namespace = configNamespace
			mergedAlertConfigMap.Data = defaultAlertConfigMap.Data
			err := r.updateMergedAlertConfigMap(ctx, mergedAlertConfigMap)
			if err != nil {
				return err
			}
		} else {
			return err
		}
	} else {
		klog.Errorf("######3")
		mergedAlertConfigMap := mergedAlertConfigMap(defaultAlertConfigMap, customAlertConfigMap)
		err := r.updateMergedAlertConfigMap(ctx, mergedAlertConfigMap)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *MulticlusterGlobalHubReconciler) updateMergedAlertConfigMap(ctx context.Context, alertConfigMap *corev1.ConfigMap) error {
	configNamespace := config.GetDefaultNamespace()
	curAlertConfigMap, err := r.KubeClient.CoreV1().ConfigMaps(configNamespace).Get(ctx, alertConfigMap.Name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			klog.Infof("creating alert configmap, namespace: %v, name: %v", configNamespace, alertConfigMap.Name)
			_, err := r.KubeClient.CoreV1().ConfigMaps(configNamespace).Create(ctx, alertConfigMap, metav1.CreateOptions{})
			if err != nil {
				return err
			}
		} else {
			return err
		}
	}
	klog.Errorf("#####nu")
	if reflect.DeepEqual(curAlertConfigMap, alertConfigMap) {
		return nil
	}
	klog.Errorf("#####up")
	klog.Infof("Update alert configmap, namespace: %v, name: %v", configNamespace, alertConfigMap.Name)
	_, err = r.KubeClient.CoreV1().ConfigMaps(configNamespace).Update(ctx, alertConfigMap, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	return nil
}

func LoadResourceFromJSON(json string) (*unstructured.Unstructured, error) {
	obj := unstructured.Unstructured{}
	err := obj.UnmarshalJSON([]byte(json))
	return &obj, err
}

func mergeYamlArray(a, b []byte) ([]interface{}, error) {
	var o1, o2 []interface{}

	if err := yaml.Unmarshal(a, &o1); err != nil {
		return nil, err
	}

	if err := yaml.Unmarshal(b, &o2); err != nil {
		return nil, err
	}

	for _, v := range o2 {
		o1 = append(o1, v)
	}

	return o1, nil
}

// mergeAlertYaml
func mergeAlertYaml(a, b []byte) ([]byte, error) {
	var o1, o2 map[string]interface{}
	if len(a) == 0 {
		return b, nil
	}
	if len(b) == 0 {
		return a, nil
	}

	if err := yaml.Unmarshal(a, &o1); err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(b, &o2); err != nil {
		return nil, err
	}

	for k, v := range o2 {
		if !(k == "groups" || k == "policies" || k == "contactPoints") {
			continue
		}
		klog.Errorf("#####:%v", v)
		klog.Errorf("#####:%v", o1[k])
		o1Array, _ := o1[k].([]interface{})
		o2Array, _ := v.([]interface{})

		o1[k] = append(o1Array, o2Array...)

		klog.Errorf("#####:%v", o1[k])

	}
	return yaml.Marshal(o1)
}

func mergedAlertConfigMap(defaultAlertConfigMap, customAlertConfigMap *corev1.ConfigMap) *corev1.ConfigMap {
	mergedAlertYaml, err := mergeAlertYaml([]byte(defaultAlertConfigMap.Data[alertConfigMapKey]), []byte(customAlertConfigMap.Data[alertConfigMapKey]))
	if err != nil {
		return nil
	}
	klog.Errorf("####m:%v", string(mergedAlertYaml))
	var mergedAlertConfigMap corev1.ConfigMap
	mergedAlertConfigMap.Name = mergedAlertName
	mergedAlertConfigMap.Namespace = config.GetDefaultNamespace()
	mergedAlertConfigMap.Data = map[string]string{
		alertConfigMapKey: string(mergedAlertYaml),
	}
	return &mergedAlertConfigMap
}

// GenerateGrafanaDataSource is used to generate the GrafanaDatasource as a secret.
// the GrafanaDatasource points to multicluster-global-hub cr
func (r *MulticlusterGlobalHubReconciler) GenerateGrafanaDataSourceSecret(
	ctx context.Context,
	mgh *globalhubv1alpha4.MulticlusterGlobalHub,
) (string, error) {
	postgresSecret, err := r.KubeClient.CoreV1().Secrets(mgh.Namespace).Get(
		ctx, operatorconstants.GHStorageSecretName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	datasourceVal, err := GrafanaDataSource(string(postgresSecret.Data["database_uri_with_readonlyuser"]),
		postgresSecret.Data["ca.crt"])
	if err != nil {
		datasourceVal, err = GrafanaDataSource(string(postgresSecret.Data["database_uri"]),
			postgresSecret.Data["ca.crt"])
		if err != nil {
			return "", err
		}
	}

	dsSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "multicluster-global-hub-grafana-datasources",
			Namespace: config.GetDefaultNamespace(),
			Labels: map[string]string{
				"datasource/time-tarted":         time.Now().Format("2006-1-2.1504"),
				constants.GlobalHubOwnerLabelKey: constants.GHOperatorOwnerLabelVal,
			},
		},
		Type: "Opaque",
		Data: map[string][]byte{
			datasourceKey: datasourceVal,
		},
	}

	// Set MGH instance as the owner and controller
	if err = controllerutil.SetControllerReference(mgh, dsSecret, r.Scheme); err != nil {
		return dsSecret.GetName(), err
	}

	// Check if this already exists
	grafanaDSFound := &corev1.Secret{}
	err = r.Client.Get(ctx, types.NamespacedName{
		Name:      dsSecret.Name,
		Namespace: dsSecret.Namespace,
	}, grafanaDSFound)

	if err != nil && errors.IsNotFound(err) {
		if err := r.Client.Create(ctx, dsSecret); err != nil {
			return dsSecret.GetName(), err
		}
	} else if err != nil {
		return dsSecret.GetName(), err
	}

	if grafanaDSFound.Data == nil {
		grafanaDSFound.Data = make(map[string][]byte)
	}
	grafanaDSFound.Data[datasourceKey] = datasourceVal
	if err = r.Client.Update(ctx, grafanaDSFound); err != nil {
		return dsSecret.GetName(), err
	}

	return dsSecret.GetName(), nil
}

func GrafanaDataSource(databaseURI string, cert []byte) ([]byte, error) {
	postgresURI := string(databaseURI)
	objURI, err := url.Parse(postgresURI)
	if err != nil {
		return nil, err
	}
	password, ok := objURI.User.Password()
	if !ok {
		return nil, fmt.Errorf("failed to get password from database_uri: %s", postgresURI)
	}

	// get the database from the objURI
	database := "hoh"
	paths := strings.Split(objURI.Path, "/")
	if len(paths) > 1 {
		database = paths[1]
	}

	ds := &GrafanaDatasource{
		Name:      "Global-Hub-DataSource",
		Type:      "postgres",
		Access:    "proxy",
		IsDefault: true,
		URL:       objURI.Host,
		User:      objURI.User.Username(),
		Database:  database,
		Editable:  false,
		JSONData: &JsonData{
			QueryTimeout: "300s",
			TimeInterval: "30s",
		},
		SecureJSONData: &SecureJsonData{
			Password: password,
		},
	}

	if len(cert) > 0 {
		ds.JSONData.SSLMode = objURI.Query().Get("sslmode") // sslmode == "verify-full" || sslmode == "verify-ca"
		ds.JSONData.TLSAuth = true
		ds.JSONData.TLSAuthWithCACert = true
		ds.JSONData.TLSSkipVerify = true
		ds.JSONData.TLSConfigurationMethod = "file-content"
		ds.SecureJSONData.TLSCACert = string(cert)
	}
	grafanaDatasources, err := yaml.Marshal(GrafanaDatasources{
		APIVersion:  1,
		Datasources: []*GrafanaDatasource{ds},
	})
	if err != nil {
		return nil, err
	}
	return grafanaDatasources, nil
}

type GrafanaDatasources struct {
	APIVersion  int                  `yaml:"apiVersion,omitempty"`
	Datasources []*GrafanaDatasource `yaml:"datasources,omitempty"`
}

type GrafanaDatasource struct {
	Access            string          `yaml:"access,omitempty"`
	BasicAuth         bool            `yaml:"basicAuth,omitempty"`
	BasicAuthPassword string          `yaml:"basicAuthPassword,omitempty"`
	BasicAuthUser     string          `yaml:"basicAuthUser,omitempty"`
	Editable          bool            `yaml:"editable,omitempty"`
	IsDefault         bool            `yaml:"isDefault,omitempty"`
	Name              string          `yaml:"name,omitempty"`
	OrgID             int             `yaml:"orgId,omitempty"`
	Type              string          `yaml:"type,omitempty"`
	URL               string          `yaml:"url,omitempty"`
	Database          string          `yaml:"database,omitempty"`
	User              string          `yaml:"user,omitempty"`
	Version           int             `yaml:"version,omitempty"`
	JSONData          *JsonData       `yaml:"jsonData,omitempty"`
	SecureJSONData    *SecureJsonData `yaml:"secureJsonData,omitempty"`
}

type JsonData struct {
	SSLMode                string `yaml:"sslmode,omitempty"`
	TLSAuth                bool   `yaml:"tlsAuth,omitempty"`
	TLSAuthWithCACert      bool   `yaml:"tlsAuthWithCACert,omitempty"`
	TLSConfigurationMethod string `yaml:"tlsConfigurationMethod,omitempty"`
	TLSSkipVerify          bool   `yaml:"tlsSkipVerify,omitempty"`
	QueryTimeout           string `yaml:"queryTimeout,omitempty"`
	HttpMethod             string `yaml:"httpMethod,omitempty"`
	TimeInterval           string `yaml:"timeInterval,omitempty"`
}

type SecureJsonData struct {
	Password      string `yaml:"password,omitempty"`
	TLSCACert     string `yaml:"tlsCACert,omitempty"`
	TLSClientCert string `yaml:"tlsClientCert,omitempty"`
	TLSClientKey  string `yaml:"tlsClientKey,omitempty"`
}
