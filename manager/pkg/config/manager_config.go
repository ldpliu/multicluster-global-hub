// Copyright (c) 2023 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package config

import (
	"time"

	"github.com/stolostron/multicluster-global-hub/manager/pkg/nonk8sapi"
	commonobjects "github.com/stolostron/multicluster-global-hub/pkg/objects"
	"github.com/stolostron/multicluster-global-hub/pkg/statistics"
	"github.com/stolostron/multicluster-global-hub/pkg/transport"
)

type ManagerConfig struct {
	ManagerNamespace      string
	WatchNamespace        string
	SchedulerInterval     string
	SyncerConfig          *SyncerConfig
	DatabaseConfig        *DatabaseConfig
	TransportConfig       *transport.TransportInternalConfig
	StatisticsConfig      *statistics.StatisticsConfig
	NonK8sAPIServerConfig *nonk8sapi.NonK8sAPIServerConfig
	ElectionConfig        *commonobjects.LeaderElectionConfig
	EnableGlobalResource  bool
	ImportClusterInHosted bool
	WithACM               bool
	LaunchJobNames        string
	EnablePprof           bool
}

type SyncerConfig struct {
	SpecSyncInterval              time.Duration
	StatusSyncInterval            time.Duration
	DeletedLabelsTrimmingInterval time.Duration
}

type DatabaseConfig struct {
	ProcessDatabaseURL         string
	TransportBridgeDatabaseURL string
	CACertPath                 string
	MaxOpenConns               int
	DataRetention              int
}
