/*
Copyright The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package kaitogarbagecollection

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/awslabs/operatorpkg/singleton"
	"github.com/awslabs/operatorpkg/status"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/samber/lo"
	"go.uber.org/multierr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/clock"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	v1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"
	"sigs.k8s.io/karpenter/pkg/metrics"
	"sigs.k8s.io/karpenter/pkg/operator/injection"
)

const (
	NodeHeartBeatAnnotationKey = "NodeHeartBeatTimeStamp"
)

type Controller struct {
	clock         clock.Clock
	kubeClient    client.Client
	cloudProvider cloudprovider.CloudProvider
}

func NewController(c clock.Clock, kubeClient client.Client, cloudProvider cloudprovider.CloudProvider) *Controller {
	return &Controller{
		clock:         c,
		kubeClient:    kubeClient,
		cloudProvider: cloudProvider,
	}
}

func (c *Controller) Reconcile(ctx context.Context) (reconcile.Result, error) {
	ctx = injection.WithControllerName(ctx, "nodeclaim.garbagecollection")

	merr := c.reconcileNodeClaims(ctx)
	nerr := c.reconcileNodes(ctx)

	return reconcile.Result{RequeueAfter: time.Minute * 2}, multierr.Combine(merr, nerr)
}

func (c *Controller) reconcileNodes(ctx context.Context) error {
	nodeList := &corev1.NodeList{}
	if err := c.kubeClient.List(ctx, nodeList); err != nil {
		return err
	}

	wsNodeClaimList := &v1.NodeClaimList{}
	if err := c.kubeClient.List(ctx, wsNodeClaimList, client.HasLabels([]string{"kaito.sh/workspace"})); err != nil {
		return err
	}

	ragNodeClaimList := &v1.NodeClaimList{}
	if err := c.kubeClient.List(ctx, ragNodeClaimList, client.HasLabels([]string{"kaito.sh/ragengine"})); err != nil {
		return err
	}

	nodeClaimsCombined := append(wsNodeClaimList.Items, ragNodeClaimList.Items...)

	nodeClaimNames := sets.NewString()
	for i := range nodeClaimsCombined {
		nodeClaimNames.Insert(nodeClaimsCombined[i].Name)
	}

	deletedGarbageNodes := lo.Filter(lo.ToSlicePtr(nodeList.Items), func(n *corev1.Node, _ int) bool {
		_, ok := n.Labels[v1.NodePoolLabelKey]
		if ok && n.Spec.ProviderID != "" {
			// We rely on a strong assumption here: the machine name is equal to the agent pool name.
			apName := n.Labels["kubernetes.azure.com/agentpool"]
			if nodeClaimNames.Has(apName) {
				return true
			}
		}
		return false
	})

	errs := make([]error, 0)
	workqueue.ParallelizeUntil(ctx, 20, len(deletedGarbageNodes), func(i int) {
		// We delete nodes to trigger the node finalization and deletion flow.
		if err := c.kubeClient.Delete(ctx, deletedGarbageNodes[i]); client.IgnoreNotFound(err) != nil {
			errs = append(errs, err)
			return
		}
		log.FromContext(ctx).Info("garbage collecting node since the linked nodeClaim does not exist", "node", deletedGarbageNodes[i].Name)
	})
	return multierr.Combine(errs...)
}

// gpu-provisioner: leverage the two minutes perodic check to update nodeClaims readiness heartbeat.
func (c *Controller) reconcileNodeClaims(ctx context.Context) error {
	wsNodeClaimList := &v1.NodeClaimList{}
	if err := c.kubeClient.List(ctx, wsNodeClaimList, client.HasLabels([]string{"kaito.sh/workspace"})); err != nil {
		return err
	}

	ragNodeClaimList := &v1.NodeClaimList{}
	if err := c.kubeClient.List(ctx, ragNodeClaimList, client.HasLabels([]string{"kaito.sh/ragengine"})); err != nil {
		return err
	}

	nodeClaimListCombined := append(wsNodeClaimList.Items, ragNodeClaimList.Items...)

	// The NotReady nodes are excluded from the list.
	cloudProviderNodeClaims, err := c.cloudProvider.List(ctx)
	if err != nil {
		return err
	}
	cloudProviderNodeClaims = lo.Filter(cloudProviderNodeClaims, func(m *v1.NodeClaim, _ int) bool {
		return m.DeletionTimestamp.IsZero()
	})
	cloudProviderProviderIDs := sets.New[string](lo.Map(cloudProviderNodeClaims, func(m *v1.NodeClaim, _ int) string {
		return m.Status.ProviderID
	})...)

	deletedGarbageNodeClaims := lo.Filter(lo.ToSlicePtr(nodeClaimListCombined), func(nc *v1.NodeClaim, _ int) bool {
		// The assumption is that any clean up work should be done in 10 minutes.
		// The node gc will cover any problems caused by this force deletion.
		return !nc.DeletionTimestamp.IsZero() && metav1.Now().After((*nc.DeletionTimestamp).Add(time.Minute*10))
	})

	rfErrs := c.batchDeleteNodeClaims(ctx, deletedGarbageNodeClaims, true, "to be delete but blocked by finializer for more than 10 minutes")

	// Check all nodeClaims heartbeats.
	hbNodeClaims := lo.Filter(lo.ToSlicePtr(nodeClaimListCombined), func(nc *v1.NodeClaim, _ int) bool {
		return nc.StatusConditions().Get(v1.ConditionTypeLaunched).IsTrue() &&
			nc.DeletionTimestamp.IsZero()
	})

	var hbUpdated atomic.Uint64
	deletedNotReadyNodeClaims := []*v1.NodeClaim{}
	// Update machine heartbeat.
	hbErrs := make([]error, 0)
	workqueue.ParallelizeUntil(ctx, 20, len(hbNodeClaims), func(i int) {
		updated := hbNodeClaims[i].DeepCopy()

		if cloudProviderProviderIDs.Has(updated.Status.ProviderID) {
			hbUpdated.Add(1)
			if updated.Annotations == nil {
				updated.Annotations = make(map[string]string)
			}

			timeStr, _ := metav1.NewTime(time.Now()).MarshalJSON()
			updated.Annotations[NodeHeartBeatAnnotationKey] = string(timeStr)

			// If the machine was not ready, it becomes ready after getting the heartbeat.
			updated.StatusConditions().SetTrue(status.ConditionReady)
		} else {
			log.FromContext(ctx).Info(fmt.Sprintf("nodeClaim %s does not receive hb", hbNodeClaims[i].Name))
			updated.StatusConditions().SetTrueWithReason(status.ConditionReady, "NodeNotReady", "Node status is NotReady")
		}
		statusCopy := updated.DeepCopy()
		updateCopy := updated.DeepCopy()
		if err := c.kubeClient.Patch(ctx, updated, client.MergeFrom(hbNodeClaims[i])); err != nil && client.IgnoreNotFound(err) != nil {
			hbErrs = append(hbErrs, err)
			return
		}
		if err := c.kubeClient.Status().Patch(ctx, statusCopy, client.MergeFrom(hbNodeClaims[i])); err != nil && client.IgnoreNotFound(err) != nil {
			hbErrs = append(hbErrs, err)
			return
		}
		if !updateCopy.StatusConditions().Get(status.ConditionReady).IsTrue() &&
			c.clock.Since(updateCopy.StatusConditions().Get(status.ConditionReady).LastTransitionTime.Time) > time.Minute*10 {
			deletedNotReadyNodeClaims = append(deletedNotReadyNodeClaims, updateCopy)
		}
	})
	if len(hbNodeClaims) > 0 {
		log.FromContext(ctx).Info(fmt.Sprintf("Update heartbeat for nodeClaims", "updated", hbUpdated.Load(), "total", len(hbNodeClaims)))
	}
	errs := c.batchDeleteNodeClaims(ctx, deletedNotReadyNodeClaims, false, "being NotReady for more than 10 minutes")
	errs = append(errs, hbErrs...)
	errs = append(errs, rfErrs...)
	return multierr.Combine(errs...)
}

func (c *Controller) batchDeleteNodeClaims(ctx context.Context, nodeClaims []*v1.NodeClaim, removeFinalizer bool, msg string) []error {
	errs := make([]error, 0)
	workqueue.ParallelizeUntil(ctx, 20, len(nodeClaims), func(i int) {
		if removeFinalizer {
			updated := nodeClaims[i].DeepCopy()
			controllerutil.RemoveFinalizer(updated, v1.TerminationFinalizer)
			if !equality.Semantic.DeepEqual(nodeClaims[i], updated) {
				if err := c.kubeClient.Patch(ctx, updated, client.MergeFrom(nodeClaims[i])); client.IgnoreNotFound(err) != nil {
					errs = append(errs, err)
					return
				}
			}
		} else if err := c.kubeClient.Delete(ctx, nodeClaims[i]); client.IgnoreNotFound(err) != nil {
			errs = append(errs, err)
			return
		}
		log.FromContext(ctx).WithValues(
			"nodepool", nodeClaims[i].Labels[v1.NodePoolLabelKey], "nodeClaim", nodeClaims[i].Name, "provider-id", nodeClaims[i].Status.ProviderID).Info("garbage collecting nodeClaims", "reason", msg)
		metrics.NodeClaimsTerminatedTotal.With(prometheus.Labels{
			metrics.NodePoolLabel:     nodeClaims[i].Labels[v1.NodePoolLabelKey],
			metrics.CapacityTypeLabel: nodeClaims[i].Labels[v1.CapacityTypeLabelKey],
		}).Inc()
	})
	return errs
}

func (c *Controller) Register(_ context.Context, m manager.Manager) error {
	return controllerruntime.NewControllerManagedBy(m).
		Named("nodeclaim.kaitogarbagecollection").
		WatchesRawSource(singleton.Source()).
		Complete(singleton.AsReconciler(c))
}
