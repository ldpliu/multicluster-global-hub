# Clusterinstance+ibi+ztp+gitops cluster migration

## Prerequest(OCP/ACM/GitOps/TALO) in Source hub(hub1) and Target hub(hub2)
1. Install ACM 2.14
    - enable siteconfig component in mch 
    - enable assisted-service and image-based-install-operator in mce
    - create `AgentServiceConfig`
    ```yaml
    apiVersion: agent-install.openshift.io/v1beta1
    kind: AgentServiceConfig
    metadata:
    creationTimestamp: "2025-10-31T03:32:04Z"
    finalizers:
    - agentserviceconfig.agent-install.openshift.io/ai-deprovision
    - agentserviceconfig.agent-install.openshift.io/local-cluster-import-deprovision
    generation: 1
    name: agent
    resourceVersion: "3299661"
    uid: cc16e4f8-f58b-45fe-8cc4-3ba5b5a9a7b0
    spec:
    databaseStorage:
        accessModes:
        - ReadWriteOnce
        resources:
        requests:
            storage: 10Gi
    filesystemStorage:
        accessModes:
        - ReadWriteOnce
        resources:
        requests:
            storage: 100Gi
    imageStorage:
        accessModes:
        - ReadWriteOnce
        resources:
        requests:
            storage: 50Gi
    ```
    check assisted service and site config enabled.
    ```
    [root@ice02 yaml]# oc get po -A|grep assi
    multicluster-engine                                assisted-image-service-0                                          1/1     Running       0               4d23h
    multicluster-engine                                assisted-service-557f75f666-dmhlb                                 2/2     Running       0               4d23h

    [root@ice02 yaml]# oc get po -A|grep site
    open-cluster-management                            siteconfig-controller-manager-7dcffc49bb-w6hcl                    1/1     Running       0               6d18h
    ```
1. [Enable ACM observability](https://github.com/stolostron/multicluster-observability-operator?tab=readme-ov-file#run-the-operator-in-the-cluster)
1. Install latest `Openshift Gitops Operator` and `Topology Aware Lifecycle Operator`
1. [Preparation of ZTP GIT repository](https://github.com/openshift-kni/telco-reference/blob/main/telco-ran/configuration/argocd/README.md#preparation-of-ztp-git-repository)
1. [Preparation of Hub cluster for ZTP](https://github.com/openshift-kni/telco-reference/blob/main/telco-ran/configuration/argocd/README.md#preparation-of-hub-cluster-for-ztp)

## clusterinstance + gitops + policy in source hub
### Prepare clusterinstance in gitops
update the clusterinstance field and put it to `argocd/cluster/ztp-clusters-01-ice01` directory
```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: vm005
---
apiVersion: v1
kind: Secret
metadata:
  name: vm005-bmc-secret
  namespace: vm005
type: Opaque
data:
  password: cGFzc3dvcmQ=
  username: YWRtaW4=
---
apiVersion: v1
kind: Secret
metadata:
  name: assisted-deployment-pull-secret
  namespace: vm005
type: kubernetes.io/dockerconfigjson
data:
---
apiVersion: siteconfig.open-cluster-management.io/v1alpha1
kind: ClusterInstance
metadata:
  name: "vm005-clusterinstance"
  namespace: "vm005"
spec:
  baseDomain: qe.red-chesterfield.com
  clusterType: "SNO"
  holdInstallation: false
  pullSecretRef:
    name: "assisted-deployment-pull-secret"
  clusterName: "vm005"
  clusterImageSetNameRef: img4.19.15-x86-64-appsub
  templateRefs:
  - name: ibi-cluster-templates-v1
    namespace: open-cluster-management
  extraLabels:
    ManagedCluster:
      common: "true"
      du-profile: latest
      group-du-sno: ""
      sites: vm008
  nodes:
  - hostName: "vm005"
    automatedCleaningMode: metadata
    bmcAddress: redfish-virtualmedia://192.168.123.1:8000/redfish/v1/Systems/42874335-b518-4185-b950-6be568cb1a38
    bootMACAddress: 52:54:00:27:8d:f3
    bootMode: "UEFI"
    bmcCredentialsName:
      name: "vm005-bmc-secret"
    templateRefs:
      - name: ibi-node-templates-v1
        namespace: open-cluster-management
```
### Create argo applications to sync clusterinstance
update `spec.source` to your git server and clusterinstance path, then apply it.

``` yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: ztp-clusters-01-ice01
  namespace: openshift-gitops
spec:
  destination:
    namespace: ztp-clusters-01-ice01
    server: 'https://kubernetes.default.svc'
  ignoreDifferences:
    - group: cluster.open-cluster-management.io
      kind: ManagedCluster
      managedFieldsManagers:
        - controller
  project: ztp-app-project
  source:
    path: ztp/gitops-subscriptions/argocd/cluster/ztp-clusters-01-ice01
    repoURL: 'http://testadmin:testadmin@10.1.228.102:3000/testadmin/cnf-features-deploy.git'
    targetRevision: ztp-global-test
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
      - CreateNamespace=true
      - PrunePropagationPolicy=background
      - RespectIgnoreDifferences=true
```
check infraenv and hostinventory in acm ui?
check host
### Prepare policygentemplate in gitops
    Update `argocd/example/policygentemplates` to set policygentemplate
### Create argo applications to sync policy
```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: policies
  namespace: openshift-gitops
spec:
  destination:
    namespace: policies-sub
    server: 'https://kubernetes.default.svc'
  project: policy-app-project
  source:
    path: ztp/gitops-subscriptions/argocd/example/policygentemplates
    repoURL: 'http://testadmin:testadmin@10.1.228.102:3000/testadmin/cnf-features-deploy.git'
    targetRevision: ztp-global-test
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
      - CreateNamespace=true
```
copy all cluster-monitorting-config configmap from hub1 and hub2 and managedcluster

check how many policy created, compoliant, from policy define, check policy definiton if oprator install on sno?
## Migrate ztp managedclusters to target hub
- Install Globalhub (pr not merged: https://github.com/stolostron/multicluster-global-hub/pull/2035 )
- Import Hub2 to hub1 as managedhub with label: `global-hub.open-cluster-management.io/deploy-mode`
- Create cluster migration cr:
```yaml
apiVersion: global-hub.open-cluster-management.io/v1alpha1
kind: ManagedClusterMigration
metadata:
  name: m1
  labels:
    global-hub.open-cluster-management.io/migration-mode: "ztp"
spec:
  from: local-cluster
  to: ice02-1
  includedManagedClusters:
  - vm008
  supportedConfigs:
    stageTimeout: 5m
```
Globalhub will migrate the following resources to target hub.
/***************************/

check the hostinventoiry
policy  same. compliance/ operator


## [apply clusterinstance and policy in gitops](#Create argo applications to sync clusterinstance)

### verify migration success
1. All resources status is right
```
# oc get clusterinstance -A
NAMESPACE   NAME                    PAUSED   PROVISIONSTATUS   PROVISIONDETAILS         AGE
vm008       vm008-clusterinstance            Completed         Provisioning completed   47h

# oc get clusterdeployment -A
NAMESPACE   NAME    INFRAID       PLATFORM        REGION   VERSION   CLUSTERTYPE   PROVISIONSTATUS   POWERSTATE   AGE
vm008       vm008   vm008-g6s8v   none-platform            4.19.17                 Provisioned       Running      47h

# oc get mcl -A
NAME            HUB ACCEPTED   MANAGED CLUSTER URLS                               JOINED   AVAILABLE   AGE
vm008           true           https://api.vm008.qe.red-chesterfield.com:6443     True     True        28h
```
2. Add a label to managedcluster by updating clusterinstance in [git repo](# Prepare clusterinstance in gitops)
`spec.extraLabels`
[Chanel discuss](https://redhat-internal.slack.com/archives/C079FT4GBE1/p1762178257608249?thread_ts=1762176071.412759&cid=C079FT4GBE1)
Make sure the label synced to managedcluster after a few minutes(5min)

3. Check all policy status is right
```
[root@ice02 yaml]# oc get policy -A
NAMESPACE   NAME                                        REMEDIATION ACTION   COMPLIANCE STATE   AGE
default     upgrade                                     inform               Compliant          26h
vm008       default.upgrade                             inform               Compliant          26h
vm008       ztp-group.group-du-sno-clo5-cleanup         inform               Compliant          22h
vm008       ztp-site.example-sno-latest-config-policy   inform               NonCompliant       22h
```
4. Use cluster group upgrade to upgrade the managedcluster version.
[example](https://github.com/openshift-kni/cluster-group-upgrades-operator/tree/main/samples)
Apply the following yaml, then update `ClusterGroupUpgrade.spec.enable: true`, the managed cluster should start upgrade. And after some minites, the upgrade should success.
```
---
apiVersion: policy.open-cluster-management.io/v1
kind: Policy
metadata:
  name: upgrade
  namespace: default
spec:
  disabled: false
  policy-templates:
  - objectDefinition:
      apiVersion: policy.open-cluster-management.io/v1
      kind: ConfigurationPolicy
      metadata:
        name: upgrade
      spec:
        namespaceselector:
          exclude:
          - kube-*
          include:
          - '*'
        object-templates:
        - complianceType: musthave
          objectDefinition:
            apiVersion: config.openshift.io/v1
            kind: ClusterVersion
            metadata:
              name: version
            spec:
              channel: stable-4.9
              desiredUpdate:
                version: 4.19.17
              upstream: https://api.openshift.com/api/upgrades_info/v1/graph
            status:
              history:
                - state: Completed
                  version: 4.19.17
        remediationAction: inform
        severity: low
  remediationAction: inform

---
apiVersion: policy.open-cluster-management.io/v1
kind: PlacementBinding
metadata:
  name: upgrade
  namespace: default
placementRef:
  apiGroup: apps.open-cluster-management.io
  kind: PlacementRule
  name: upgrade
subjects:
- apiGroup: policy.open-cluster-management.io
  kind: Policy
  name: upgrade

---
---
apiVersion: apps.open-cluster-management.io/v1
kind: PlacementRule
metadata:
  name: upgrade
  namespace: default
spec:
  clusters:
  - name: sno
```

```
apiVersion: ran.openshift.io/v1alpha1
kind: ClusterGroupUpgrade
metadata:
  name: test
  namespace: default
spec:
  clusters:
  - sno
  managedPolicies:
  - upgrade
  remediationStrategy:
    maxConcurrency: 1
  enable: false
```
5. Alert 
