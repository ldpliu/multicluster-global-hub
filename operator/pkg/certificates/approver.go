// Copyright (c) Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project
// Licensed under the Apache License 2.0

package certificates

import (
	"strings"

	certificatesv1 "k8s.io/api/certificates/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

func approve(cluster *clusterv1.ManagedCluster, csr *certificatesv1.CertificateSigningRequest,
) bool {
	if strings.HasPrefix(csr.Spec.Username, "system:open-cluster-management:"+cluster.Name) {
		log.Info("CSR approved")
		return true
	} else {
		log.Info("CSR not approved due to illegal requester", "requester", csr.Spec.Username)
		return false
	}
}
