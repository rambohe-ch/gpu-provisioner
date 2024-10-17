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

package nodeclaim

import (
	"context"
	"errors"
	"fmt"

	"github.com/awslabs/operatorpkg/object"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

const (
	WorkspaceLabelKey = "kaito.sh/workspace"
	RagEngineLabelKey = "kaito.sh/ragengine"
)

var (
	selector1, _ = predicate.LabelSelectorPredicate(metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{
			{Key: WorkspaceLabelKey, Operator: metav1.LabelSelectorOpExists},
		},
	})
	selector2, _ = predicate.LabelSelectorPredicate(metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{
			{Key: RagEngineLabelKey, Operator: metav1.LabelSelectorOpExists},
		},
	})
	KaitoNodeClaimPredicate = predicate.Or(selector1, selector2)

	KaitoNodeClaimLabels = []string{WorkspaceLabelKey, RagEngineLabelKey}
)

// PodEventHandler is a watcher on corev1.Pods that maps Pods to NodeClaim based on the node names
// and enqueues reconcile.Requests for the NodeClaims
func PodEventHandler(c client.Client) handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) (requests []reconcile.Request) {
		if name := o.(*corev1.Pod).Spec.NodeName; name != "" {
			node := &corev1.Node{}
			if err := c.Get(ctx, types.NamespacedName{Name: name}, node); err != nil {
				return []reconcile.Request{}
			}
			nodeClaimList := &v1.NodeClaimList{}
			if err := c.List(ctx, nodeClaimList, client.MatchingFields{"status.providerID": node.Spec.ProviderID}); err != nil {
				return []reconcile.Request{}
			}
			return lo.Map(nodeClaimList.Items, func(n v1.NodeClaim, _ int) reconcile.Request {
				return reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(&n),
				}
			})
		}
		return requests
	})
}

// NodeEventHandler is a watcher on corev1.Node that maps Nodes to NodeClaims based on provider ids
// and enqueues reconcile.Requests for the NodeClaims
func NodeEventHandler(c client.Client) handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []reconcile.Request {
		node := o.(*corev1.Node)
		nodeClaimList := &v1.NodeClaimList{}
		if err := c.List(ctx, nodeClaimList, client.MatchingFields{"status.providerID": node.Spec.ProviderID}); err != nil {
			return []reconcile.Request{}
		}
		return lo.Map(nodeClaimList.Items, func(n v1.NodeClaim, _ int) reconcile.Request {
			return reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(&n),
			}
		})
	})
}

// NodePoolEventHandler is a watcher on v1.NodeClaim that maps NodePool to NodeClaims based
// on the v1.NodePoolLabelKey and enqueues reconcile.Requests for the NodeClaim
func NodePoolEventHandler(c client.Client) handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) (requests []reconcile.Request) {
		nodeClaimList := &v1.NodeClaimList{}
		if err := c.List(ctx, nodeClaimList, client.MatchingLabels(map[string]string{v1.NodePoolLabelKey: o.GetName()})); err != nil {
			return requests
		}
		return lo.Map(nodeClaimList.Items, func(n v1.NodeClaim, _ int) reconcile.Request {
			return reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(&n),
			}
		})
	})
}

// NodeClassEventHandler is a watcher on v1.NodeClaim that maps NodeClass to NodeClaims based
// on the nodeClassRef and enqueues reconcile.Requests for the NodeClaim
func NodeClassEventHandler(c client.Client) handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) (requests []reconcile.Request) {
		nodeClaimList := &v1.NodeClaimList{}
		if err := c.List(ctx, nodeClaimList, client.MatchingFields{
			"spec.nodeClassRef.group": object.GVK(o).Group,
			"spec.nodeClassRef.kind":  object.GVK(o).Kind,
			"spec.nodeClassRef.name":  o.GetName(),
		}); err != nil {
			return requests
		}
		return lo.Map(nodeClaimList.Items, func(n v1.NodeClaim, _ int) reconcile.Request {
			return reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(&n),
			}
		})
	})
}

// NodeNotFoundError is an error returned when no corev1.Nodes are found matching the passed providerID
type NodeNotFoundError struct {
	ProviderID string
}

func (e *NodeNotFoundError) Error() string {
	return fmt.Sprintf("no nodes found for provider id '%s'", e.ProviderID)
}

func IsNodeNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	nnfErr := &NodeNotFoundError{}
	return errors.As(err, &nnfErr)
}

func IgnoreNodeNotFoundError(err error) error {
	if !IsNodeNotFoundError(err) {
		return err
	}
	return nil
}

// DuplicateNodeError is an error returned when multiple corev1.Nodes are found matching the passed providerID
type DuplicateNodeError struct {
	ProviderID string
}

func (e *DuplicateNodeError) Error() string {
	return fmt.Sprintf("multiple found for provider id '%s'", e.ProviderID)
}

func IsDuplicateNodeError(err error) bool {
	if err == nil {
		return false
	}
	dnErr := &DuplicateNodeError{}
	return errors.As(err, &dnErr)
}

func IgnoreDuplicateNodeError(err error) error {
	if !IsDuplicateNodeError(err) {
		return err
	}
	return nil
}

// NodeForNodeClaim is a helper function that takes a v1.NodeClaim and attempts to find the matching corev1.Node by its providerID
// This function will return errors if:
//  1. No corev1.Nodes match the v1.NodeClaim providerID
//  2. Multiple corev1.Nodes match the v1.NodeClaim providerID
func NodeForNodeClaim(ctx context.Context, c client.Client, nodeClaim *v1.NodeClaim) (*corev1.Node, error) {
	nodes, err := AllNodesForNodeClaim(ctx, c, nodeClaim)
	if err != nil {
		return nil, err
	}
	if len(nodes) > 1 {
		return nil, &DuplicateNodeError{ProviderID: nodeClaim.Status.ProviderID}
	}
	if len(nodes) == 0 {
		return nil, &NodeNotFoundError{ProviderID: nodeClaim.Status.ProviderID}
	}
	return nodes[0], nil
}

// AllNodesForNodeClaim is a helper function that takes a v1.NodeClaim and finds ALL matching corev1.Nodes by their providerID
// If the providerID is not resolved for a NodeClaim, then no Nodes will map to it
func AllNodesForNodeClaim(ctx context.Context, c client.Client, nodeClaim *v1.NodeClaim) ([]*corev1.Node, error) {
	// NodeClaims that have no resolved providerID have no nodes mapped to them
	if nodeClaim.Status.ProviderID == "" {
		// check common failures caused by bad input
		// cond := nodeClaim.StatusConditions().Get(v1.ConditionTypeLaunched)
		// if cond != nil && !cond.IsTrue() && (cond.Message == "all requested instance types were unavailable during launch" || strings.Contains(cond.Message, "is not allowed in your subscription in location")) {
		// 	return nil, nil // Not recoverable, does not consider as an error
		// } else {
		// 	return nil, fmt.Errorf("the nodeClaim(%s) has not been associated with any node yet, message: %s", nodeClaim.Name, cond.Message)
		// }
		return nil, nil
	}

	nodeList := corev1.NodeList{}
	if err := c.List(ctx, &nodeList, client.MatchingFields{"spec.providerID": nodeClaim.Status.ProviderID}); err != nil {
		return nil, fmt.Errorf("listing nodes, %w", err)
	}
	return lo.ToSlicePtr(nodeList.Items), nil
}

func UpdateNodeOwnerReferences(nodeClaim *v1.NodeClaim, node *corev1.Node) *corev1.Node {
	gvk := object.GVK(nodeClaim)
	node.OwnerReferences = append(node.OwnerReferences, metav1.OwnerReference{
		APIVersion:         gvk.GroupVersion().String(),
		Kind:               gvk.Kind,
		Name:               nodeClaim.Name,
		UID:                nodeClaim.UID,
		BlockOwnerDeletion: lo.ToPtr(true),
	})
	return node
}

func AllKaitoNodeClaims(ctx context.Context, c client.Client) ([]v1.NodeClaim, error) {
	kaitoNodeClaims := make([]v1.NodeClaim, 0)

	for i := range KaitoNodeClaimLabels {
		nodeClaimList := &v1.NodeClaimList{}
		if err := c.List(ctx, nodeClaimList, client.HasLabels([]string{KaitoNodeClaimLabels[i]})); err != nil {
			return kaitoNodeClaims, err
		}

		kaitoNodeClaims = append(kaitoNodeClaims, nodeClaimList.Items...)
	}
	return kaitoNodeClaims, nil
}
