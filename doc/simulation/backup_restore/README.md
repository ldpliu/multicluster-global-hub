ACM Official doc:
https://access.redhat.com/documentation/en-us/red_hat_advanced_cluster_management_for_kubernetes/2.8/html/business_continuity/business-cont-overview#backup-intro

Prerequest for this script:
1. [Prepare for aws bucket](https://access.redhat.com/documentation/en-us/openshift_container_platform/4.13/html-single/backup_and_restore/index#installing-oadp-aws)
2. [Prepare restic repo](https://restic.readthedocs.io/en/stable/030_preparing_a_new_repo.html#amazon-s3)


# Backup
## Install ACM and Globalhub(include mgh cr)
## Start backup
```shell
export AWS_ACCESS_KEY_ID=''
export AWS_SECRET_ACCESS_KEY=''
export AWS_BUCKET=''

export RESTIC_REPO=''
export  RESTIC_PASSWD=''

./backupenv.sh
```
## check backup successful
```
oc get backup -A
[sh]# oc get backup -A
NAMESPACE                        NAME                                            AGE
open-cluster-management-backup   acm-credentials-schedule-20231218053605         14m
open-cluster-management-backup   acm-managed-clusters-schedule-20231218053605    14m
open-cluster-management-backup   acm-resources-generic-schedule-20231218053605   14m
open-cluster-management-backup   acm-resources-schedule-20231218053605           14m
open-cluster-management-backup   acm-validation-policy-schedule-20231218053605   14m

```

```
[sh]# oc get replicationsource -A
NAMESPACE                 NAME                                            SOURCE                                          LAST SYNC              DURATION          NEXT SYNC
multicluster-global-hub   data-0-kafka-kafka-0                            data-0-kafka-kafka-0                            2024-01-05T07:15:21Z   1m44.580899401s   2024-01-05T08:00:00Z
multicluster-global-hub   data-0-kafka-kafka-1                            data-0-kafka-kafka-1                            2024-01-05T07:15:04Z   1m27.556178147s   2024-01-05T08:00:00Z
multicluster-global-hub   data-0-kafka-kafka-2                            data-0-kafka-kafka-2                            2024-01-05T07:14:53Z   1m16.530639672s   2024-01-05T08:00:00Z
multicluster-global-hub   data-kafka-zookeeper-0                          data-kafka-zookeeper-0                          2024-01-05T07:14:53Z   1m16.537957882s   2024-01-05T08:00:00Z
multicluster-global-hub   data-kafka-zookeeper-1                          data-kafka-zookeeper-1                          2024-01-05T07:14:53Z   1m16.529819149s   2024-01-05T08:00:00Z
multicluster-global-hub   data-kafka-zookeeper-2                          data-kafka-zookeeper-2                          2024-01-05T07:15:04Z   1m27.545185626s   2024-01-05T08:00:00Z
multicluster-global-hub   postgresdb-multicluster-global-hub-postgres-0   postgresdb-multicluster-global-hub-postgres-0   2024-01-05T07:15:24Z   1m47.599200873s   2024-01-05T08:00:00Z
```

## Stop backup
```
oc delete -f backup/schedule-acm.yaml
```

# Restore:
## Install ACM and globalhub operator(do not include mgh)
## Start restore (passive)
```sh
export AWS_ACCESS_KEY_ID=''
export AWS_SECRET_ACCESS_KEY=''
export AWS_BUCKET=''

./restoreenv.sh

```
## Check restore
```sh
oc get restores.velero.io -A
NAMESPACE                        NAME                                                                     AGE
open-cluster-management-backup   restore-acm-passive-sync-acm-credentials-schedule-20240105071259         4m33s
open-cluster-management-backup   restore-acm-passive-sync-acm-resources-generic-schedule-20240105071259   4m33s
open-cluster-management-backup   restore-acm-passive-sync-acm-resources-schedule-20240105071259           4m33s
```
```sh
oc get ReplicationDestination
NAME                                                 LAST SYNC              DURATION        NEXT SYNC
b-multicluster-global-hub-postgres-020240105091547   2024-01-05T09:21:11Z   39.49297454s
data-0-kafka-kafka-020240105091547                   2024-01-05T09:21:10Z   31.736655798s
data-0-kafka-kafka-120240105091547                   2024-01-05T09:21:10Z   31.685438457s
data-0-kafka-kafka-220240105091547                   2024-01-05T09:21:10Z   38.696521499s
data-kafka-zookeeper-020240105091547                 2024-01-05T09:21:10Z   38.648918174s
data-kafka-zookeeper-120240105091547                 2024-01-05T09:21:10Z   38.661557684s
data-kafka-zookeeper-220240105091547                 2024-01-05T09:21:10Z   38.703553106s
```

```sh
oc get pvc -A
NAMESPACE                 NAME                                                               STATUS   VOLUME                                     CAPACITY   ACCESS MODES   STORAGECLASS   AGE
multicluster-global-hub   data-0-kafka-kafka-0                                               Bound    pvc-551855ab-50d1-4cb5-a766-31ca57eb4a1c   10Gi       RWO            gp3-csi        7m9s
multicluster-global-hub   data-0-kafka-kafka-1                                               Bound    pvc-b337e68d-df74-447f-9130-5ee55dc72a89   10Gi       RWO            gp3-csi        7m9s
multicluster-global-hub   data-0-kafka-kafka-2                                               Bound    pvc-233739e5-390c-4a6f-ac25-6d8bef327bf8   10Gi       RWO            gp3-csi        7m9s
multicluster-global-hub   data-kafka-zookeeper-0                                             Bound    pvc-6a7aa9b9-dda4-4610-9366-0acc02f449c5   10Gi       RWO            gp3-csi        7m9s
multicluster-global-hub   data-kafka-zookeeper-1                                             Bound    pvc-ff90c297-8bcf-41d1-a1bb-8079a1f46c55   10Gi       RWO            gp3-csi        7m9s
multicluster-global-hub   data-kafka-zookeeper-2                                             Bound    pvc-d66fc894-de05-41fa-b3d7-39c67ecff213   10Gi       RWO            gp3-csi        7m9s
multicluster-global-hub   postgresdb-multicluster-global-hub-postgres-0                      Bound    pvc-a7aebea3-8d86-40df-a61e-fdf2ac95d0ef   25Gi       RWO            gp3-csi        7m9s
multicluster-global-hub   volsync-b-multicluster-global-hub-postgres-020240105091547-cache   Bound    pvc-2376eeaf-dc32-4326-9c7b-d5ede983dfe1   1Gi        RWO            gp3-csi        6m59s
multicluster-global-hub   volsync-data-0-kafka-kafka-020240105091547-cache                   Bound    pvc-36a8b1ea-a0d5-44db-b508-377ebbd5dde7   1Gi        RWO            gp3-csi        6m58s
multicluster-global-hub   volsync-data-0-kafka-kafka-120240105091547-cache                   Bound    pvc-6dbc70c6-11b2-4b84-866a-2bb456c90a4a   1Gi        RWO            gp3-csi        6m58s
multicluster-global-hub   volsync-data-0-kafka-kafka-220240105091547-cache                   Bound    pvc-dce1ca1e-d09b-434e-b6d4-bcdc9cb5dccb   1Gi        RWO            gp3-csi        6m59s
multicluster-global-hub   volsync-data-kafka-zookeeper-020240105091547-cache                 Bound    pvc-de80215a-8bde-4d0d-9c55-5dea5ebbcb7c   1Gi        RWO            gp3-csi        6m59s
multicluster-global-hub   volsync-data-kafka-zookeeper-120240105091547-cache                 Bound    pvc-70bc5884-28c4-4b70-b8fc-5ed7635aad95   1Gi        RWO            gp3-csi        6m59s
multicluster-global-hub   volsync-data-kafka-zookeeper-220240105091547-cache                 Bound    pvc-cd1f174a-35bb-4985-837a-45eea5e6b690   1Gi        RWO            gp3-csi        6m59s

```

## Start restore (activity)
oc delete -f restore/restore.yaml
oc apply -f restore/restore-active.yaml

