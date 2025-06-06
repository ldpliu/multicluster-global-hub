// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package controllers

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/equality"
	channelv1 "open-cluster-management.io/multicloud-operators-channel/pkg/apis/apps/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/stolostron/multicluster-global-hub/manager/pkg/spec/specdb"
	"github.com/stolostron/multicluster-global-hub/pkg/constants"
	"github.com/stolostron/multicluster-global-hub/pkg/logger"
)

func AddChannelController(mgr ctrl.Manager, specDB specdb.SpecDB) error {
	if err := ctrl.NewControllerManagedBy(mgr).
		For(&channelv1.Channel{}).
		WithEventFilter(GlobalResourcePredicate()).
		WithEventFilter(predicate.NewPredicateFuncs(func(obj client.Object) bool {
			ownerReferences := obj.GetOwnerReferences()
			for _, reference := range ownerReferences {
				if kind := reference.Kind; kind == constants.MultiClusterHubKind {
					return false
				}
			}
			return true
		})).
		Complete(&genericSpecController{
			client:         mgr.GetClient(),
			specDB:         specDB,
			log:            logger.ZapLogger("channels-spec-controller"),
			tableName:      "channels",
			finalizerName:  constants.GlobalHubCleanupFinalizer,
			createInstance: func() client.Object { return &channelv1.Channel{} },
			cleanObject:    cleanChannelStatus,
			areEqual:       areChannelsEqual,
		}); err != nil {
		return fmt.Errorf("failed to add channel controller to the manager: %w", err)
	}

	return nil
}

func cleanChannelStatus(instance client.Object) {
	channel, ok := instance.(*channelv1.Channel)
	if !ok {
		panic("wrong instance passed to cleanChannelStatus: not a Channel")
	}

	channel.Status = channelv1.ChannelStatus{}
}

func areChannelsEqual(instance1, instance2 client.Object) bool {
	channel1, ok1 := instance1.(*channelv1.Channel)
	channel2, ok2 := instance2.(*channelv1.Channel)

	if !ok1 || !ok2 {
		return false
	}

	specMatch := equality.Semantic.DeepEqual(channel1.Spec, channel2.Spec)
	annotationsMatch := equality.Semantic.DeepEqual(instance1.GetAnnotations(), instance2.GetAnnotations())
	labelsMatch := equality.Semantic.DeepEqual(instance1.GetLabels(), instance2.GetLabels())

	return specMatch && annotationsMatch && labelsMatch
}
