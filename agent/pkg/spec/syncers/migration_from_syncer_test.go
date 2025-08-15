// Copyright (c) 2024 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package syncers

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	klusterletv1alpha1 "github.com/stolostron/cluster-lifecycle-api/klusterletconfig/v1alpha1"
	addonv1 "github.com/stolostron/klusterlet-addon-controller/pkg/apis/agent/v1"
	mchv1 "github.com/stolostron/multiclusterhub-operator/api/v1"
	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	operatorv1 "open-cluster-management.io/api/operator/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/stolostron/multicluster-global-hub/agent/pkg/configs"
	migrationv1alpha1 "github.com/stolostron/multicluster-global-hub/operator/api/migration/v1alpha1"
	"github.com/stolostron/multicluster-global-hub/pkg/bundle/migration"
	eventversion "github.com/stolostron/multicluster-global-hub/pkg/bundle/version"
	"github.com/stolostron/multicluster-global-hub/pkg/constants"
	"github.com/stolostron/multicluster-global-hub/pkg/enum"
	"github.com/stolostron/multicluster-global-hub/pkg/transport"
	"github.com/stolostron/multicluster-global-hub/pkg/transport/controller"
	"github.com/stolostron/multicluster-global-hub/pkg/utils"
)

// go test -run ^TestMigrationSourceHubSyncer$ github.com/stolostron/multicluster-global-hub/agent/pkg/spec/syncers -v
func TestMigrationSourceHubSyncer(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("Failed to add corev1 to scheme: %v", err)
	}
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("Failed to add clientgoscheme to scheme: %v", err)
	}
	if err := clusterv1.AddToScheme(scheme); err != nil {
		t.Fatalf("Failed to add clusterv1 to scheme: %v", err)
	}
	if err := clusterv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("Failed to add clusterv1beta1 to scheme: %v", err)
	}
	if err := operatorv1.AddToScheme(scheme); err != nil {
		t.Fatalf("Failed to add operatorv1 to scheme: %v", err)
	}
	if err := klusterletv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("Failed to add klusterletv1alpha1 to scheme: %v", err)
	}
	if err := addonv1.SchemeBuilder.AddToScheme(scheme); err != nil {
		t.Fatalf("Failed to add addonv1 to scheme: %v", err)
	}
	if err := mchv1.SchemeBuilder.AddToScheme(scheme); err != nil {
		t.Fatalf("Failed to add mchv1 to scheme: %v", err)
	}

	currentSyncerMigrationId := "020340324302432049234023040320"

	cases := []struct {
		name                         string
		receivedMigrationEventBundle migration.MigrationSourceBundle
		initObjects                  []client.Object
		expectedProduceEvent         *cloudevents.Event
		expectedObjects              []client.Object
	}{
		{
			name: "Initializing: migrate cluster1 from hub1 to hub2",
			initObjects: []client.Object{
				&clusterv1.ManagedCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cluster1",
					},
					Spec: clusterv1.ManagedClusterSpec{
						HubAcceptsClient:     true,
						LeaseDurationSeconds: 60,
					},
				},
				&addonv1.KlusterletAddonConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cluster1",
						Namespace: "cluster1",
					},
				},
				&mchv1.MultiClusterHub{
					ObjectMeta: metav1.ObjectMeta{
						Name: "multiclusterhub",
					},
					Spec: mchv1.MultiClusterHubSpec{},
					Status: mchv1.MultiClusterHubStatus{
						CurrentVersion: "2.13.0",
					},
				},
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-configmap",
						Namespace: "cluster1",
						UID:       "020340324302432049234023040320",
					},
					Data: map[string]string{"hello": "world"},
				},
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-secret",
						Namespace: "cluster1",
						UID:       "020340324302432049234023040321",
					},
					StringData: map[string]string{"test": "secret"},
				},
			},
			receivedMigrationEventBundle: migration.MigrationSourceBundle{
				MigrationId: currentSyncerMigrationId,
				ToHub:       "hub2",
				Stage:       migrationv1alpha1.PhaseInitializing,
				BootstrapSecret: &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: bootstrapSecretNamePrefix + "hub2", Namespace: "multicluster-engine"},
				},
				ManagedClusters: []string{"cluster1"},
			},
			expectedProduceEvent: func() *cloudevents.Event {
				// report to global hub deploying status
				configs.SetAgentConfig(&configs.AgentConfig{LeafHubName: "hub1"})

				evt := cloudevents.NewEvent()
				evt.SetType(string(enum.ManagedClusterMigrationType)) // spec message: initialize -> deploy
				evt.SetSource("hub1")
				evt.SetExtension(constants.CloudEventExtensionKeyClusterName, "global-hub")
				evt.SetExtension(eventversion.ExtVersion, "0.1")
				return &evt
			}(),
		},
		{
			name: "Registering: migrate cluster1 from hub1 to hub2",
			initObjects: []client.Object{
				&clusterv1.ManagedCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cluster1",
					},
					Spec: clusterv1.ManagedClusterSpec{
						HubAcceptsClient:     true,
						LeaseDurationSeconds: 60,
					},
				},
				&mchv1.MultiClusterHub{
					ObjectMeta: metav1.ObjectMeta{
						Name: "multiclusterhub",
					},
					Spec: mchv1.MultiClusterHubSpec{},
					Status: mchv1.MultiClusterHubStatus{
						CurrentVersion: "2.14.0",
					},
				},
			},
			receivedMigrationEventBundle: migration.MigrationSourceBundle{
				MigrationId:     currentSyncerMigrationId,
				ToHub:           "hub2",
				Stage:           migrationv1alpha1.PhaseRegistering,
				ManagedClusters: []string{"cluster1"},
			},
			expectedProduceEvent: nil,
			expectedObjects: []client.Object{
				&clusterv1.ManagedCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cluster1",
					},
					Spec: clusterv1.ManagedClusterSpec{
						HubAcceptsClient:     false,
						LeaseDurationSeconds: 60,
					},
				},
			},
		},
		{
			name: "Cleaned up: migrate cluster1 from hub1 to hub2",
			initObjects: []client.Object{
				&clusterv1.ManagedCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cluster1",
					},
					Spec: clusterv1.ManagedClusterSpec{
						HubAcceptsClient:     true,
						LeaseDurationSeconds: 60,
					},
				},
				&mchv1.MultiClusterHub{
					ObjectMeta: metav1.ObjectMeta{
						Name: "multiclusterhub",
					},
					Spec: mchv1.MultiClusterHubSpec{},
					Status: mchv1.MultiClusterHubStatus{
						CurrentVersion: "2.14.0",
					},
				},
			},
			receivedMigrationEventBundle: migration.MigrationSourceBundle{
				MigrationId: currentSyncerMigrationId,
				ToHub:       "hub2",
				Stage:       migrationv1alpha1.PhaseCleaning,
				BootstrapSecret: &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      bootstrapSecretNamePrefix + "hub2",
						Namespace: "multicluster-engine",
					},
					Data: map[string][]byte{
						"test1": []byte(`payload`),
					},
				},
				ManagedClusters: []string{"cluster1"},
			},
			expectedProduceEvent: func() *cloudevents.Event {
				configs.SetAgentConfig(&configs.AgentConfig{LeafHubName: "hub1"})

				evt := cloudevents.NewEvent()
				evt.SetType(string(enum.ManagedClusterMigrationType))
				evt.SetSource("hub1")
				evt.SetExtension(constants.CloudEventExtensionKeyClusterName, "hub2")
				evt.SetExtension(eventversion.ExtVersion, "0.1")
				_ = evt.SetData(*cloudevents.StringOfApplicationCloudEventsJSON(), &migration.MigrationStatusBundle{
					Stage: migrationv1alpha1.ConditionTypeCleaned,
				})
				return &evt
			}(),
			expectedObjects: nil,
		},
		{
			name: "Rollback initializing: rollback cluster1 migration annotations",
			initObjects: []client.Object{
				&clusterv1.ManagedCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cluster1",
						Annotations: map[string]string{
							constants.ManagedClusterMigrating: "global-hub.open-cluster-management.io/migrating",
							KlusterletConfigAnnotation:        "migration-hub2",
						},
					},
					Spec: clusterv1.ManagedClusterSpec{
						HubAcceptsClient:     true,
						LeaseDurationSeconds: 60,
					},
				},
				&mchv1.MultiClusterHub{
					ObjectMeta: metav1.ObjectMeta{
						Name: "multiclusterhub",
					},
					Spec: mchv1.MultiClusterHubSpec{},
					Status: mchv1.MultiClusterHubStatus{
						CurrentVersion: "2.14.0",
					},
				},
			},
			receivedMigrationEventBundle: migration.MigrationSourceBundle{
				MigrationId:     currentSyncerMigrationId,
				ToHub:           "hub2",
				Stage:           migrationv1alpha1.PhaseRollbacking,
				RollbackStage:   migrationv1alpha1.PhaseInitializing,
				ManagedClusters: []string{"cluster1"},
				BootstrapSecret: &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: bootstrapSecretNamePrefix + "hub2", Namespace: "multicluster-engine"},
					Type:       corev1.SecretTypeOpaque,
					StringData: map[string]string{"test": "secret"},
				},
			},
			expectedProduceEvent: func() *cloudevents.Event {
				configs.SetAgentConfig(&configs.AgentConfig{LeafHubName: "hub1"})

				evt := cloudevents.NewEvent()
				evt.SetType(string(enum.ManagedClusterMigrationType))
				evt.SetSource("hub1")
				evt.SetExtension(constants.CloudEventExtensionKeyClusterName, "global-hub")
				evt.SetExtension(eventversion.ExtVersion, "0.1")
				return &evt
			}(),
			expectedObjects: []client.Object{
				&clusterv1.ManagedCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "cluster1",
						Annotations: map[string]string{},
					},
					Spec: clusterv1.ManagedClusterSpec{
						HubAcceptsClient:     true,
						LeaseDurationSeconds: 60,
					},
				},
			},
		},
		{
			name: "Registering: cluster already has HubAcceptsClient false - should skip",
			initObjects: []client.Object{
				&clusterv1.ManagedCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cluster1",
					},
					Spec: clusterv1.ManagedClusterSpec{
						HubAcceptsClient:     false, // Already false
						LeaseDurationSeconds: 60,
					},
				},
				&mchv1.MultiClusterHub{
					ObjectMeta: metav1.ObjectMeta{
						Name: "multiclusterhub",
					},
					Spec: mchv1.MultiClusterHubSpec{},
					Status: mchv1.MultiClusterHubStatus{
						CurrentVersion: "2.14.0",
					},
				},
			},
			receivedMigrationEventBundle: migration.MigrationSourceBundle{
				MigrationId:     currentSyncerMigrationId,
				ToHub:           "hub2",
				Stage:           migrationv1alpha1.PhaseRegistering,
				ManagedClusters: []string{"cluster1"},
			},
			expectedProduceEvent: nil,
			expectedObjects: []client.Object{
				&clusterv1.ManagedCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cluster1",
					},
					Spec: clusterv1.ManagedClusterSpec{
						HubAcceptsClient:     false, // Should remain false
						LeaseDurationSeconds: 60,
					},
				},
			},
		},
		{
			name: "Rollback registering: restore HubAcceptsClient to true and clean up",
			initObjects: []client.Object{
				&clusterv1.ManagedCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cluster1",
						Annotations: map[string]string{
							constants.ManagedClusterMigrating: "global-hub.open-cluster-management.io/migrating",
							KlusterletConfigAnnotation:        "migration-hub2",
						},
					},
					Spec: clusterv1.ManagedClusterSpec{
						HubAcceptsClient:     false, // Set to false during registering
						LeaseDurationSeconds: 60,
					},
				},
				&mchv1.MultiClusterHub{
					ObjectMeta: metav1.ObjectMeta{
						Name: "multiclusterhub",
					},
					Spec: mchv1.MultiClusterHubSpec{},
					Status: mchv1.MultiClusterHubStatus{
						CurrentVersion: "2.14.0",
					},
				},
			},
			receivedMigrationEventBundle: migration.MigrationSourceBundle{
				MigrationId:     currentSyncerMigrationId,
				ToHub:           "hub2",
				Stage:           migrationv1alpha1.PhaseRollbacking,
				RollbackStage:   migrationv1alpha1.PhaseRegistering,
				ManagedClusters: []string{"cluster1"},
				BootstrapSecret: &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: bootstrapSecretNamePrefix + "hub2", Namespace: "multicluster-engine"},
					Type:       corev1.SecretTypeOpaque,
					StringData: map[string]string{"test": "secret"},
				},
			},
			expectedProduceEvent: func() *cloudevents.Event {
				configs.SetAgentConfig(&configs.AgentConfig{LeafHubName: "hub1"})

				evt := cloudevents.NewEvent()
				evt.SetType(string(enum.ManagedClusterMigrationType))
				evt.SetSource("hub1")
				evt.SetExtension(constants.CloudEventExtensionKeyClusterName, "global-hub")
				evt.SetExtension(eventversion.ExtVersion, "0.1")
				return &evt
			}(),
			expectedObjects: []client.Object{
				&clusterv1.ManagedCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "cluster1",
						Annotations: map[string]string{}, // Annotations should be cleaned up
					},
					Spec: clusterv1.ManagedClusterSpec{
						HubAcceptsClient:     true, // Should be set to true during rollback
						LeaseDurationSeconds: 60,
					},
				},
			},
		},
		{
			name: "Rollback deploying: rollback cluster1 migration after deploy failure",
			initObjects: []client.Object{
				&clusterv1.ManagedCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cluster1",
						Annotations: map[string]string{
							constants.ManagedClusterMigrating: "global-hub.open-cluster-management.io/migrating",
							KlusterletConfigAnnotation:        "migration-hub2",
						},
					},
					Spec: clusterv1.ManagedClusterSpec{
						HubAcceptsClient:     true,
						LeaseDurationSeconds: 60,
					},
				},
				&mchv1.MultiClusterHub{
					ObjectMeta: metav1.ObjectMeta{
						Name: "multiclusterhub",
					},
					Spec: mchv1.MultiClusterHubSpec{},
					Status: mchv1.MultiClusterHubStatus{
						CurrentVersion: "2.14.0",
					},
				},
			},
			receivedMigrationEventBundle: migration.MigrationSourceBundle{
				MigrationId:     currentSyncerMigrationId,
				ToHub:           "hub2",
				Stage:           migrationv1alpha1.PhaseRollbacking,
				RollbackStage:   migrationv1alpha1.PhaseDeploying,
				ManagedClusters: []string{"cluster1"},
				BootstrapSecret: &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: bootstrapSecretNamePrefix + "hub2", Namespace: "multicluster-engine"},
					Type:       corev1.SecretTypeOpaque,
					StringData: map[string]string{"test": "secret"},
				},
			},
			expectedProduceEvent: func() *cloudevents.Event {
				configs.SetAgentConfig(&configs.AgentConfig{LeafHubName: "hub1"})

				evt := cloudevents.NewEvent()
				evt.SetType(string(enum.ManagedClusterMigrationType))
				evt.SetSource("hub1")
				evt.SetExtension(constants.CloudEventExtensionKeyClusterName, "global-hub")
				evt.SetExtension(eventversion.ExtVersion, "0.1")
				return &evt
			}(),
			expectedObjects: []client.Object{
				&clusterv1.ManagedCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "cluster1",
						Annotations: map[string]string{},
					},
					Spec: clusterv1.ManagedClusterSpec{
						HubAcceptsClient:     true,
						LeaseDurationSeconds: 60,
					},
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(c.initObjects...).WithObjects(
				c.initObjects...).Build()

			producer := ProducerMock{}
			transportClient := &controller.TransportClient{}
			transportClient.SetProducer(&producer)

			transportConfig := &transport.TransportInternalConfig{
				TransportType: string(transport.Chan),
				KafkaCredential: &transport.KafkaConfig{
					SpecTopic: "spec",
				},
			}

			managedClusterMigrationSyncer := NewMigrationSourceSyncer(fakeClient, nil, transportClient,
				transportConfig)
			managedClusterMigrationSyncer.currentMigrationId = currentSyncerMigrationId
			payload, err := json.Marshal(c.receivedMigrationEventBundle)
			assert.Nil(t, err)
			if err != nil {
				t.Errorf("Failed to marshal payload of managed cluster migration: %v", err)
			}

			// sync managed cluster migration
			evt := utils.ToCloudEvent(constants.MigrationTargetMsgKey, constants.CloudEventGlobalHubClusterName,
				"hub2", payload)
			err = managedClusterMigrationSyncer.Sync(ctx, &evt)
			if err != nil {
				t.Errorf("Failed to sync managed cluster migration: %v", err)
			}

			sentEvent := producer.sentEvent
			expectEvent := c.expectedProduceEvent
			if expectEvent != nil {
				assert.Equal(t, sentEvent.Type(), expectEvent.Type())
				assert.Equal(t, sentEvent.Type(), expectEvent.Type())
				assert.Equal(t, sentEvent.Source(), expectEvent.Source())
				assert.Equal(t, sentEvent.Extensions(), sentEvent.Extensions())
			}

			if c.expectedObjects != nil {
				for _, obj := range c.expectedObjects {
					runtimeObj := obj.DeepCopyObject().(client.Object)
					err = fakeClient.Get(ctx, client.ObjectKeyFromObject(obj), runtimeObj)
					assert.Nil(t, err)
					assert.True(t, apiequality.Semantic.DeepDerivative(obj, runtimeObj))
				}
			}
		})
	}
}

func TestGenerateKlusterletConfig(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("Failed to add clientgoscheme to scheme: %v", err)
	}
	_ = mchv1.AddToScheme(scheme)
	cases := []struct {
		name                string
		mch                 *mchv1.MultiClusterHub
		targetHub           string
		initObjects         []client.Object
		bootstrapSecretName string
		expectedError       bool
		expected213         bool
	}{
		{
			name: "MCH version 2.13",
			mch: &mchv1.MultiClusterHub{
				Status: mchv1.MultiClusterHubStatus{
					CurrentVersion: "2.13.0",
				},
			},
			targetHub:           "hub2",
			bootstrapSecretName: "bootstrap-hub2",
			expectedError:       false,
			expected213:         true,
			initObjects: []client.Object{
				&mchv1.MultiClusterHub{
					ObjectMeta: metav1.ObjectMeta{
						Name: "multiclusterhub",
					},
					Spec: mchv1.MultiClusterHubSpec{},
					Status: mchv1.MultiClusterHubStatus{
						CurrentVersion: "2.13.0",
					},
				},
			},
		},
		{
			name: "MCH version 2.14",
			mch: &mchv1.MultiClusterHub{
				Status: mchv1.MultiClusterHubStatus{
					CurrentVersion: "2.14.1",
				},
			},
			targetHub:           "hub2",
			bootstrapSecretName: "bootstrap-hub2",
			expectedError:       false,
			expected213:         false,
			initObjects: []client.Object{
				&mchv1.MultiClusterHub{
					ObjectMeta: metav1.ObjectMeta{
						Name: "multiclusterhub",
					},
					Spec: mchv1.MultiClusterHubSpec{},
					Status: mchv1.MultiClusterHubStatus{
						CurrentVersion: "2.14.2",
					},
				},
			},
		},
		{
			name:                "No MCH found",
			mch:                 nil,
			targetHub:           "hub2",
			bootstrapSecretName: "bootstrap-hub2",
			expectedError:       true,
		},
		{
			name:                "No MCH status found",
			mch:                 nil,
			targetHub:           "hub2",
			bootstrapSecretName: "bootstrap-hub2",
			expectedError:       false,
			expected213:         false,
			initObjects: []client.Object{
				&mchv1.MultiClusterHub{
					ObjectMeta: metav1.ObjectMeta{
						Name: "multiclusterhub",
					},
					Spec: mchv1.MultiClusterHubSpec{},
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(c.initObjects...).WithObjects(
				c.initObjects...).Build()
			obj, err := generateKlusterletConfig(fakeClient, c.targetHub, c.bootstrapSecretName)
			if c.expectedError {
				assert.NotNil(t, err)
			} else {
				assert.Nil(t, err)
				byte, _ := yaml.Marshal(obj)
				if c.expected213 {
					assert.True(t, !strings.Contains(string(byte), "multipleHubsConfig"))
				} else {
					assert.True(t, strings.Contains(string(byte), "multipleHubsConfig"))
				}
			}
		})
	}
}

type ProducerMock struct {
	sentEvent *cloudevents.Event
}

func (m *ProducerMock) SendEvent(ctx context.Context, evt cloudevents.Event) error {
	m.sentEvent = &evt
	return nil
}

func (m *ProducerMock) Reconnect(config *transport.TransportInternalConfig, topic string) error {
	return nil
}

// TestReportMigrationStatus tests the ReportMigrationStatus function
func TestReportMigrationStatus(t *testing.T) {
	tests := []struct {
		name                string
		migrationBundle     *migration.MigrationStatusBundle
		transportClient     transport.TransportClient
		version             *eventversion.Version
		expectedError       bool
		expectedEventType   string
		expectedSource      string
		expectedClusterName string
	}{
		{
			name: "successful status report",
			migrationBundle: &migration.MigrationStatusBundle{
				MigrationId: "test-migration-123",
				Stage:       migrationv1alpha1.PhaseInitializing,
				ErrMessage:  "",
			},
			transportClient: func() transport.TransportClient {
				producer := &ProducerMock{}
				transportClient := &controller.TransportClient{}
				transportClient.SetProducer(producer)
				return transportClient
			}(),
			version:             eventversion.NewVersion(),
			expectedError:       false,
			expectedEventType:   string(enum.ManagedClusterMigrationType),
			expectedSource:      "test-hub",
			expectedClusterName: constants.CloudEventGlobalHubClusterName,
		},
		{
			name: "status report with error message",
			migrationBundle: &migration.MigrationStatusBundle{
				MigrationId: "test-migration-456",
				Stage:       migrationv1alpha1.PhaseDeploying,
				ErrMessage:  "failed to deploy cluster",
			},
			transportClient: func() transport.TransportClient {
				producer := &ProducerMock{}
				transportClient := &controller.TransportClient{}
				transportClient.SetProducer(producer)
				return transportClient
			}(),
			version:             eventversion.NewVersion(),
			expectedError:       false,
			expectedEventType:   string(enum.ManagedClusterMigrationType),
			expectedSource:      "test-hub",
			expectedClusterName: constants.CloudEventGlobalHubClusterName,
		},
		{
			name: "nil transport client",
			migrationBundle: &migration.MigrationStatusBundle{
				MigrationId: "test-migration-789",
				Stage:       migrationv1alpha1.PhaseValidating,
				ErrMessage:  "",
			},
			transportClient: nil,
			version:         eventversion.NewVersion(),
			expectedError:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up agent config for consistent source name
			configs.SetAgentConfig(&configs.AgentConfig{LeafHubName: "test-hub"})

			ctx := context.Background()
			err := ReportMigrationStatus(ctx, tt.transportClient, tt.migrationBundle, tt.version)

			if tt.expectedError {
				assert.NotNil(t, err)
				assert.Contains(t, err.Error(), "transport client must not be nil")
			} else {
				assert.Nil(t, err)

				// Verify the event was sent correctly
				if transportClient, ok := tt.transportClient.(*controller.TransportClient); ok {
					if producer, ok := transportClient.GetProducer().(*ProducerMock); ok {
						sentEvent := producer.sentEvent
						assert.NotNil(t, sentEvent)
						assert.Equal(t, tt.expectedEventType, sentEvent.Type())
						assert.Equal(t, tt.expectedSource, sentEvent.Source())
						assert.Equal(t, tt.expectedClusterName, sentEvent.Extensions()[constants.CloudEventExtensionKeyClusterName])

						// Verify version extension
						versionExt := sentEvent.Extensions()[eventversion.ExtVersion]
						assert.NotNil(t, versionExt)

						// Verify the payload contains our migration bundle
						var receivedBundle migration.MigrationStatusBundle
						err := json.Unmarshal(sentEvent.Data(), &receivedBundle)
						assert.Nil(t, err)
						assert.Equal(t, tt.migrationBundle.MigrationId, receivedBundle.MigrationId)
						assert.Equal(t, tt.migrationBundle.Stage, receivedBundle.Stage)
						assert.Equal(t, tt.migrationBundle.ErrMessage, receivedBundle.ErrMessage)
					}
				}
			}
		})
	}
}

// TestSendEvent tests the SendEvent function
func TestSendEvent(t *testing.T) {
	tests := []struct {
		name            string
		eventType       string
		source          string
		clusterName     string
		payloadBytes    []byte
		transportClient transport.TransportClient
		version         *eventversion.Version
		expectedError   bool
	}{
		{
			name:         "successful event send",
			eventType:    "test.event.type",
			source:       "test-source",
			clusterName:  "test-cluster",
			payloadBytes: []byte(`{"test": "data"}`),
			transportClient: func() transport.TransportClient {
				producer := &ProducerMock{}
				transportClient := &controller.TransportClient{}
				transportClient.SetProducer(producer)
				return transportClient
			}(),
			version:       eventversion.NewVersion(),
			expectedError: false,
		},
		{
			name:         "empty payload",
			eventType:    "test.event.type",
			source:       "test-source",
			clusterName:  "test-cluster",
			payloadBytes: []byte{},
			transportClient: func() transport.TransportClient {
				producer := &ProducerMock{}
				transportClient := &controller.TransportClient{}
				transportClient.SetProducer(producer)
				return transportClient
			}(),
			version:       eventversion.NewVersion(),
			expectedError: false,
		},
		{
			name:            "nil transport client",
			eventType:       "test.event.type",
			source:          "test-source",
			clusterName:     "test-cluster",
			payloadBytes:    []byte(`{"test": "data"}`),
			transportClient: nil,
			version:         eventversion.NewVersion(),
			expectedError:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			originalVersion := tt.version.String()

			err := SendEvent(ctx, tt.transportClient, tt.eventType, tt.source, tt.clusterName, tt.payloadBytes, tt.version)

			if tt.expectedError {
				assert.NotNil(t, err)
				assert.Contains(t, err.Error(), "transport client must not be nil")
			} else {
				assert.Nil(t, err)

				// Verify version was incremented and progressed
				assert.NotEqual(t, originalVersion, tt.version.String())

				// Verify the event was sent correctly
				if transportClient, ok := tt.transportClient.(*controller.TransportClient); ok {
					if producer, ok := transportClient.GetProducer().(*ProducerMock); ok {
						sentEvent := producer.sentEvent
						assert.NotNil(t, sentEvent)
						assert.Equal(t, tt.eventType, sentEvent.Type())
						assert.Equal(t, tt.source, sentEvent.Source())
						assert.Equal(t, tt.clusterName, sentEvent.Extensions()[constants.CloudEventExtensionKeyClusterName])

						// Verify version extension was set
						versionExt := sentEvent.Extensions()[eventversion.ExtVersion]
						assert.NotNil(t, versionExt)

						// Verify the payload
						assert.Equal(t, tt.payloadBytes, sentEvent.Data())
					}
				}
			}
		})
	}
}

// TestGetClustersFromPlacementDecisions tests the getClustersFromPlacementDecisions function
func TestGetClustersFromPlacementDecisions(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()

	if err := clusterv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("Failed to add clusterv1beta1 to scheme: %v", err)
	}

	tests := []struct {
		name             string
		placementName    string
		initObjects      []client.Object
		expectedClusters []string
		expectedError    bool
		errorContains    string
	}{
		{
			name:          "successful retrieval with multiple placement decisions",
			placementName: "test-placement",
			initObjects: []client.Object{
				&clusterv1beta1.PlacementDecision{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "placement-decision-1",
						Namespace: "default",
						Labels: map[string]string{
							clusterv1beta1.PlacementLabel: "test-placement",
						},
					},
					Status: clusterv1beta1.PlacementDecisionStatus{
						Decisions: []clusterv1beta1.ClusterDecision{
							{ClusterName: "cluster1"},
							{ClusterName: "cluster2"},
						},
					},
				},
				&clusterv1beta1.PlacementDecision{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "placement-decision-2",
						Namespace: "default",
						Labels: map[string]string{
							clusterv1beta1.PlacementLabel: "test-placement",
						},
					},
					Status: clusterv1beta1.PlacementDecisionStatus{
						Decisions: []clusterv1beta1.ClusterDecision{
							{ClusterName: "cluster3"},
						},
					},
				},
			},
			expectedClusters: []string{"cluster1", "cluster2", "cluster3"},
			expectedError:    false,
		},
		{
			name:             "no placement decisions found",
			placementName:    "nonexistent-placement",
			initObjects:      []client.Object{},
			expectedClusters: []string{},
			expectedError:    false,
		},
		{
			name:          "placement decisions with different labels",
			placementName: "target-placement",
			initObjects: []client.Object{
				&clusterv1beta1.PlacementDecision{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "placement-decision-different",
						Namespace: "default",
						Labels: map[string]string{
							clusterv1beta1.PlacementLabel: "different-placement",
						},
					},
					Status: clusterv1beta1.PlacementDecisionStatus{
						Decisions: []clusterv1beta1.ClusterDecision{
							{ClusterName: "cluster-should-not-appear"},
						},
					},
				},
				&clusterv1beta1.PlacementDecision{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "placement-decision-target",
						Namespace: "default",
						Labels: map[string]string{
							clusterv1beta1.PlacementLabel: "target-placement",
						},
					},
					Status: clusterv1beta1.PlacementDecisionStatus{
						Decisions: []clusterv1beta1.ClusterDecision{
							{ClusterName: "target-cluster"},
						},
					},
				},
			},
			expectedClusters: []string{"target-cluster"},
			expectedError:    false,
		},
		{
			name:          "placement decision with empty cluster name",
			placementName: "test-placement",
			initObjects: []client.Object{
				&clusterv1beta1.PlacementDecision{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "placement-decision-empty",
						Namespace: "default",
						Labels: map[string]string{
							clusterv1beta1.PlacementLabel: "test-placement",
						},
					},
					Status: clusterv1beta1.PlacementDecisionStatus{
						Decisions: []clusterv1beta1.ClusterDecision{
							{ClusterName: "valid-cluster"},
							{ClusterName: ""}, // This should be filtered out
							{ClusterName: "another-valid-cluster"},
						},
					},
				},
			},
			expectedClusters: []string{"valid-cluster", "another-valid-cluster"},
			expectedError:    false,
		},
		{
			name:          "placement decision with no decisions",
			placementName: "empty-placement",
			initObjects: []client.Object{
				&clusterv1beta1.PlacementDecision{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "placement-decision-no-decisions",
						Namespace: "default",
						Labels: map[string]string{
							clusterv1beta1.PlacementLabel: "empty-placement",
						},
					},
					Status: clusterv1beta1.PlacementDecisionStatus{
						Decisions: []clusterv1beta1.ClusterDecision{},
					},
				},
			},
			expectedClusters: []string{},
			expectedError:    false,
		},
		{
			name:          "placement decision without placement label",
			placementName: "test-placement",
			initObjects: []client.Object{
				&clusterv1beta1.PlacementDecision{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "placement-decision-no-label",
						Namespace: "default",
						// No placement label
					},
					Status: clusterv1beta1.PlacementDecisionStatus{
						Decisions: []clusterv1beta1.ClusterDecision{
							{ClusterName: "orphan-cluster"},
						},
					},
				},
			},
			expectedClusters: []string{},
			expectedError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.initObjects...).
				Build()

			// Create the syncer instance
			syncer := &MigrationSourceSyncer{
				client: fakeClient,
			}

			// Call the function under test
			clusters, err := syncer.getClustersFromPlacementDecisions(ctx, tt.placementName)

			// Verify error expectations
			if tt.expectedError {
				assert.NotNil(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.Nil(t, err)
			}

			// Verify cluster list
			assert.Equal(t, len(tt.expectedClusters), len(clusters), "Expected %d clusters, got %d", len(tt.expectedClusters), len(clusters))
			assert.ElementsMatch(t, tt.expectedClusters, clusters, "Cluster lists don't match")
		})
	}
}

// TestValidating tests the validating function
func TestValidating(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()

	if err := clusterv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("Failed to add clusterv1beta1 to scheme: %v", err)
	}

	tests := []struct {
		name                  string
		migrationSourceBundle *migration.MigrationSourceBundle
		initObjects           []client.Object
		expectedError         bool
		expectedErrorContains string
		expectedEventSent     bool
		expectedStatusBundle  *migration.MigrationStatusBundle
	}{
		{
			name: "successful validation with placement name and clusters found",
			migrationSourceBundle: &migration.MigrationSourceBundle{
				MigrationId:   "test-migration-123",
				Stage:         migrationv1alpha1.PhaseValidating,
				PlacementName: "test-placement",
			},
			initObjects: []client.Object{
				&clusterv1beta1.PlacementDecision{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "placement-decision-1",
						Namespace: "default",
						Labels: map[string]string{
							clusterv1beta1.PlacementLabel: "test-placement",
						},
					},
					Status: clusterv1beta1.PlacementDecisionStatus{
						Decisions: []clusterv1beta1.ClusterDecision{
							{ClusterName: "cluster1"},
							{ClusterName: "cluster2"},
						},
					},
				},
			},
			expectedError:     false,
			expectedEventSent: true,
			expectedStatusBundle: &migration.MigrationStatusBundle{
				MigrationId:     "test-migration-123",
				Stage:           migrationv1alpha1.PhaseValidating,
				ManagedClusters: []string{"cluster1", "cluster2"},
			},
		},
		{
			name: "successful validation with placement name but no clusters found",
			migrationSourceBundle: &migration.MigrationSourceBundle{
				MigrationId:   "test-migration-456",
				Stage:         migrationv1alpha1.PhaseValidating,
				PlacementName: "nonexistent-placement",
			},
			initObjects:       []client.Object{},
			expectedError:     false,
			expectedEventSent: true,
			expectedStatusBundle: &migration.MigrationStatusBundle{
				MigrationId:     "test-migration-456",
				Stage:           migrationv1alpha1.PhaseValidating,
				ManagedClusters: []string{},
			},
		},
		{
			name: "successful validation with no placement name provided",
			migrationSourceBundle: &migration.MigrationSourceBundle{
				MigrationId:   "test-migration-789",
				Stage:         migrationv1alpha1.PhaseValidating,
				PlacementName: "", // Empty placement name
			},
			initObjects:       []client.Object{},
			expectedError:     false,
			expectedEventSent: false, // No event should be sent
		},
		{
			name: "validation with empty cluster names filtered out",
			migrationSourceBundle: &migration.MigrationSourceBundle{
				MigrationId:   "test-migration-empty",
				Stage:         migrationv1alpha1.PhaseValidating,
				PlacementName: "test-placement",
			},
			initObjects: []client.Object{
				&clusterv1beta1.PlacementDecision{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "placement-decision-empty",
						Namespace: "default",
						Labels: map[string]string{
							clusterv1beta1.PlacementLabel: "test-placement",
						},
					},
					Status: clusterv1beta1.PlacementDecisionStatus{
						Decisions: []clusterv1beta1.ClusterDecision{
							{ClusterName: "valid-cluster"},
							{ClusterName: ""}, // This should be filtered out
							{ClusterName: "another-valid-cluster"},
						},
					},
				},
			},
			expectedError:     false,
			expectedEventSent: true,
			expectedStatusBundle: &migration.MigrationStatusBundle{
				MigrationId:     "test-migration-empty",
				Stage:           migrationv1alpha1.PhaseValidating,
				ManagedClusters: []string{"valid-cluster", "another-valid-cluster"},
			},
		},
		{
			name: "validation with multiple placement decisions",
			migrationSourceBundle: &migration.MigrationSourceBundle{
				MigrationId:   "test-migration-multi",
				Stage:         migrationv1alpha1.PhaseValidating,
				PlacementName: "multi-placement",
			},
			initObjects: []client.Object{
				&clusterv1beta1.PlacementDecision{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "placement-decision-1",
						Namespace: "default",
						Labels: map[string]string{
							clusterv1beta1.PlacementLabel: "multi-placement",
						},
					},
					Status: clusterv1beta1.PlacementDecisionStatus{
						Decisions: []clusterv1beta1.ClusterDecision{
							{ClusterName: "cluster1"},
							{ClusterName: "cluster2"},
						},
					},
				},
				&clusterv1beta1.PlacementDecision{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "placement-decision-2",
						Namespace: "default",
						Labels: map[string]string{
							clusterv1beta1.PlacementLabel: "multi-placement",
						},
					},
					Status: clusterv1beta1.PlacementDecisionStatus{
						Decisions: []clusterv1beta1.ClusterDecision{
							{ClusterName: "cluster3"},
						},
					},
				},
			},
			expectedError:     false,
			expectedEventSent: true,
			expectedStatusBundle: &migration.MigrationStatusBundle{
				MigrationId:     "test-migration-multi",
				Stage:           migrationv1alpha1.PhaseValidating,
				ManagedClusters: []string{"cluster1", "cluster2", "cluster3"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up agent config for consistent source name
			configs.SetAgentConfig(&configs.AgentConfig{LeafHubName: "test-hub"})

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.initObjects...).
				Build()

			// Create mock producer and transport client
			producer := &ProducerMock{}
			transportClient := &controller.TransportClient{}
			transportClient.SetProducer(producer)

			transportConfig := &transport.TransportInternalConfig{
				TransportType: string(transport.Chan),
				KafkaCredential: &transport.KafkaConfig{
					StatusTopic: "status",
				},
			}

			// Create the syncer instance
			syncer := &MigrationSourceSyncer{
				client:          fakeClient,
				transportClient: transportClient,
				transportConfig: transportConfig,
				bundleVersion:   eventversion.NewVersion(),
			}

			// Call the function under test
			err := syncer.validating(ctx, tt.migrationSourceBundle)

			// Verify error expectations
			if tt.expectedError {
				assert.NotNil(t, err)
				if tt.expectedErrorContains != "" {
					assert.Contains(t, err.Error(), tt.expectedErrorContains)
				}
			} else {
				assert.Nil(t, err)
			}

			// Verify event sending expectations
			if tt.expectedEventSent {
				assert.NotNil(t, producer.sentEvent, "Expected an event to be sent but none was sent")

				if producer.sentEvent != nil {
					// Verify the event type
					assert.Equal(t, string(enum.ManagedClusterMigrationType), producer.sentEvent.Type())

					// Verify the event data contains the expected status bundle
					var receivedBundle migration.MigrationStatusBundle
					err := json.Unmarshal(producer.sentEvent.Data(), &receivedBundle)
					assert.Nil(t, err)

					assert.Equal(t, tt.expectedStatusBundle.MigrationId, receivedBundle.MigrationId)
					assert.Equal(t, tt.expectedStatusBundle.Stage, receivedBundle.Stage)
					assert.ElementsMatch(t, tt.expectedStatusBundle.ManagedClusters, receivedBundle.ManagedClusters)
				}
			} else {
				assert.Nil(t, producer.sentEvent, "Expected no event to be sent but one was sent")
			}
		})
	}
}
