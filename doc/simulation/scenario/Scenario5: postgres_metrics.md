# Scenario 5: Postgres Metrics(release-2.11/globalhub-1.2)

## Scale

- 5 Managed Hubs, Each with 300 Managed Clusters, 50 Policies
- 1500 Managed Clusters
- 250 Policies, 75,000 Replicated Policies

## Environment

1. Install the global hub and then join the 5 simulated managed hubs into it

2. Deploy the `multicluster-global-hub-agent` to the `hub1` ~ `hub5` cluster

3. Rotating all the policies to update status, Like changing the all the status from `Compliant` to `NonCompliant`

4. Observe the metrics from the dashboard.

## Postgres Setting

- Check the Postgres statefulset and Dashboards for More Detail
  - Postgres statefulset

    ```yaml
    apiVersion: apps/v1
    kind: StatefulSet
    metadata:
      name: multicluster-global-hub-postgres
      namespace: multicluster-global-hub
    spec:
      persistentVolumeClaimRetentionPolicy:
        whenDeleted: Retain
        whenScaled: Retain
      podManagementPolicy: OrderedReady
      replicas: 1
      revisionHistoryLimit: 10
      template:
        spec:
          containers:
          - env:
            - name: POSTGRESQL_SHARED_BUFFERS
              value: 64MB
            - name: POSTGRESQL_EFFECTIVE_CACHE_SIZE
              value: 128MB
            - name: WORK_MEM
              value: 16MB
            image: quay.io/stolostron/postgresql-13:1-101
            imagePullPolicy: Always
            name: multicluster-global-hub-postgres
            resources:
              limits:
                memory: 4Gi
              requests:
                cpu: 25m
                memory: 128Mi
          - args:
            - --config.file=/etc/postgres_exporter.yml
            - --web.listen-address=:9187
            - --collector.stat_statements
            image: quay.io/prometheuscommunity/postgres-exporter:v0.15.0
            imagePullPolicy: Always
            name: prometheus-postgres-exporter
          terminationGracePeriodSeconds: 30
      volumeClaimTemplates:
      - apiVersion: v1
        kind: PersistentVolumeClaim
        metadata:
          name: postgresdb
        spec:
          accessModes:
          - ReadWriteOnce
          resources:
            requests:
              storage: 25Gi
          volumeMode: Filesystem

    ```

  - Postgres Config
  ```yaml
      postgresql.conf: |
      ssl = on
      ssl_cert_file = '/opt/app-root/src/certs/tls.crt' # server certificate
      ssl_key_file =  '/opt/app-root/src/certs/tls.key' # server private key
      shared_preload_libraries = 'pg_stat_statements'
      pg_stat_statements.max = 10000
      pg_stat_statements.track = all
  ```
  - Postgres Dashboard without Workload
  ![Broker Dashboard](./images/4-kafka-broker-dashboard-0.gif)
  ![Zookeeper Dashboard](./images/4-kafka-zookeeper-dashboard-0.png)

## Initializing and Rotating Policies

### The Count of the Global Hub Data from database

The global hub counters are used to count the managed clusters, compliances and policy events from database over time. 

- The Managed Clusters
![Manager Cluster](./images/4-count-initialization.png)

- The Compliances
![Compliances](./images/4-count-compliance.png)

- The Policy Events
![Policy Events](./images/4-count-event.png)

### Throughputs and Message Rate

![Throughputs](./images/4-kafka-throughputs.png)

- The Max Message Size: `1MB`

- Initialization: Import `5 hubs`

  - Max Incoming Byte Rate: `57.5KB`
  - Max Outgoing Byte Rate: `1.09MB`
  - Max Incoming Message Rate(messages/second): `0.5`

- Rotating Policy Status

  - Average Incoming Byte Rate: `20KB`
  - Average Outgoing Byte Rate: `20KB`
  - Average Incoming Message Rate(messages/second): `1.53`


### Other Metrics

![Broker Dashboard1](./images/4-kafka-broker-dashboard-1.1.png)
![Broker Dashboard2](./images/4-kafka-broker-dashboard-1.2.png)
![Zookeeper Dashboard](./images/4-kafka-zookeeper-dashboard-0.png)
![Kafka PVC Usage](./images/4-global-hub-kafka-pvc-usage.png)
