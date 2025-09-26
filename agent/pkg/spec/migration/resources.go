package migration

import "k8s.io/apimachinery/pkg/runtime/schema"

type MigrationResourceType struct {
	// namespace and name is same as cluster name
	gvk        schema.GroupVersionKind
	isZTP      bool
	needStatus bool
}

// when add a resource here, should also update perimssion in the following:
// operator/config/rbac/role.yaml
// operator/pkg/controllers/agent/addon/manifests/templates/agent/multicluster-global-hub-agent-clusterrole.yaml
// operator/pkg/controllers/agent/local_agent_controller.go
// operator/pkg/controllers/agent/manifests/clusterrole.yaml
var migrateResources = []MigrationResourceType{
	{
		gvk: schema.GroupVersionKind{
			Group:   "extensions.hive.openshift.io",
			Version: "v1beta1",
			Kind:    "AgentClusterInstall",
		},
		isZTP:      true,
		needStatus: true,
	},
	{
		gvk: schema.GroupVersionKind{
			Group:   "hive.openshift.io",
			Version: "v1",
			Kind:    "ClusterDeployment",
		},
		isZTP:      true,
		needStatus: true,
	},
	{
		gvk: schema.GroupVersionKind{
			Group:   "agent.open-cluster-management.io",
			Version: "v1",
			Kind:    "KlusterletAddonConfig",
		},
		isZTP:      false,
		needStatus: false,
	},
	{
		gvk: schema.GroupVersionKind{
			Group:   "",
			Version: "v1",
			Kind:    "ConfigMap",
		},
		isZTP:      false,
		needStatus: false,
	},
	{
		gvk: schema.GroupVersionKind{
			Group:   "metal3.io",
			Version: "v1alpha1",
			Kind:    "BareMetalHost",
		},
		isZTP:      true,
		needStatus: true,
	},
	{
		gvk: schema.GroupVersionKind{
			Group:   "agent-install.openshift.io",
			Version: "v1beta1",
			Kind:    "InfraEnv",
		},
		isZTP:      true,
		needStatus: true,
	},
	{
		gvk: schema.GroupVersionKind{
			Group:   "metal3.io",
			Version: "v1alpha1",
			Kind:    "HostFirmwareSettings",
		},
		isZTP:      true,
		needStatus: true,
	},
	{
		gvk: schema.GroupVersionKind{
			Group:   "agent-install.openshift.io",
			Version: "v1beta1",
			Kind:    "NMStateConfig",
		},
		isZTP:      true,
		needStatus: false,
	},
}
