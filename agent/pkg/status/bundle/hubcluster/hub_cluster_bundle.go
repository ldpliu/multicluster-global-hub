package hubcluster

import (
	"reflect"
	"sync"

	routev1 "github.com/openshift/api/route/v1"
	clustersv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"

	agentbundle "github.com/stolostron/multicluster-global-hub/agent/pkg/status/bundle"
	statusbundle "github.com/stolostron/multicluster-global-hub/pkg/bundle/status"
	"github.com/stolostron/multicluster-global-hub/pkg/constants"
)

// LeafHubClusterInfoStatusBundle creates a new instance of LeafHubClusterInfoStatusBundle.
func NewLeafHubClusterInfoStatusBundle(leafHubName string) agentbundle.Bundle {
	return &LeafHubClusterInfoStatusBundle{
		HubClusterInfoBundle: statusbundle.HubClusterInfoBundle{
			Objects:       make([]*statusbundle.LeafHubClusterInfo, 0),
			LeafHubName:   leafHubName,
			BundleVersion: statusbundle.NewBundleVersion(),
		},
		lock: sync.Mutex{},
	}
}

// LeafHubClusterInfoStatusBundle holds information for leaf hub cluster info status bundle.
type LeafHubClusterInfoStatusBundle struct {
	statusbundle.HubClusterInfoBundle
	lock sync.Mutex
}

// UpdateObject function to update a single object inside a bundle.
func (bundle *LeafHubClusterInfoStatusBundle) UpdateObject(object agentbundle.Object) {
	bundle.lock.Lock()
	defer bundle.lock.Unlock()

	if len(bundle.Objects) == 0 {

		bundle.Objects = []*statusbundle.LeafHubClusterInfo{
			{
				LeafHubName: bundle.LeafHubName,
			},
		}
	}
	var oldObj = *bundle.Objects[0]

	route, ok := object.(*routev1.Route)
	if ok {
		var routeURL string
		if len(route.Spec.Host) != 0 {
			routeURL = "https://" + route.Spec.Host
		}
		if route.GetName() == constants.OpenShiftConsoleRouteName {
			bundle.Objects[0].ConsoleURL = routeURL
		} else if route.GetName() == constants.ObservabilityGrafanaRouteName {
			bundle.Objects[0].GrafanaURL = routeURL
		}
	}

	clusterClaim, ok := object.(*clustersv1alpha1.ClusterClaim)
	if ok && clusterClaim.Name == "id.k8s.io" {
		bundle.Objects[0].ClusterId = clusterClaim.Spec.Value
	}

	if !reflect.DeepEqual(oldObj, *bundle.Objects[0]) {
		bundle.BundleVersion.Incr()
	}
}

// DeleteObject function to delete a single object inside a bundle.
func (bundle *LeafHubClusterInfoStatusBundle) DeleteObject(object agentbundle.Object) {
	// do nothing
}

// GetBundleVersion function to get bundle version.
func (bundle *LeafHubClusterInfoStatusBundle) GetBundleVersion() *statusbundle.BundleVersion {
	bundle.lock.Lock()
	defer bundle.lock.Unlock()

	return bundle.BundleVersion
}
