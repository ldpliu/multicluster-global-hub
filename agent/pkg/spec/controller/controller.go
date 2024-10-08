package controller

import (
	"context"
	"fmt"

	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/stolostron/multicluster-global-hub/agent/pkg/config"
	"github.com/stolostron/multicluster-global-hub/agent/pkg/spec/controller/syncers"
	"github.com/stolostron/multicluster-global-hub/agent/pkg/spec/controller/workers"
	"github.com/stolostron/multicluster-global-hub/pkg/constants"
	"github.com/stolostron/multicluster-global-hub/pkg/transport"
)

var specCtrlStarted = false

func AddToManager(context context.Context, mgr ctrl.Manager, consumer transport.Consumer, agentConfig *config.AgentConfig) error {
	if specCtrlStarted {
		return nil
	}

	if consumer == nil {
		klog.Info("the consumer is not initialized for the spec controller")
		return nil
	}

	// add worker pool to manager
	workers := workers.NewWorkerPool(agentConfig.SpecWorkPoolSize, mgr.GetConfig())
	if err := mgr.Add(workers); err != nil {
		return fmt.Errorf("failed to add k8s workers pool to runtime manager: %w", err)
	}

	// add bundle dispatcher to manager
	dispatcher := syncers.NewGenericDispatcher(consumer, *agentConfig)
	if err := mgr.Add(dispatcher); err != nil {
		return fmt.Errorf("failed to add bundle dispatcher to runtime manager: %w", err)
	}

	// register syncer to the dispatcher
	if agentConfig.EnableGlobalResource {
		dispatcher.RegisterSyncer(constants.GenericSpecMsgKey,
			syncers.NewGenericSyncer(workers, agentConfig))
		dispatcher.RegisterSyncer(constants.ManagedClustersLabelsMsgKey,
			syncers.NewManagedClusterLabelSyncer(workers))
	}

	dispatcher.RegisterSyncer(constants.CloudEventTypeMigrationFrom,
		syncers.NewManagedClusterMigrationFromSyncer(mgr.GetClient()))
	dispatcher.RegisterSyncer(constants.CloudEventTypeMigrationTo,
		syncers.NewManagedClusterMigrationToSyncer(mgr.GetClient()))
	dispatcher.RegisterSyncer(constants.ResyncMsgKey, syncers.NewResyncSyncer())

	specCtrlStarted = true
	return nil
}
