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

package disruption

import (
	"context"
	"fmt"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"
)

type KaitoDrift struct {
	cloudProvider cloudprovider.CloudProvider
}

func (d *KaitoDrift) Reconcile(ctx context.Context, _ *v1.NodePool, nodeClaim *v1.NodeClaim) (reconcile.Result, error) {
	hasDriftedCondition := nodeClaim.StatusConditions().Get(v1.ConditionTypeDrifted) != nil

	// From here there are three scenarios to handle:
	// 1. If NodeClaim is not launched, remove the drift status condition
	if !nodeClaim.StatusConditions().Get(v1.ConditionTypeLaunched).IsTrue() {
		_ = nodeClaim.StatusConditions().Clear(v1.ConditionTypeDrifted)
		if hasDriftedCondition {
			log.FromContext(ctx).V(1).Info("removing drift status condition, isn't launched")
		}
		return reconcile.Result{}, nil
	}

	// 2. Otherwise, if the NodeClaim isn't drifted, but has the status condition, remove it.
	driftedReason, err := d.cloudProvider.IsDrifted(ctx, nodeClaim)
	if err != nil {
		return reconcile.Result{}, cloudprovider.IgnoreNodeClaimNotFoundError(fmt.Errorf("getting drift, %w", err))
	}
	if driftedReason == "" {
		if hasDriftedCondition {
			_ = nodeClaim.StatusConditions().Clear(v1.ConditionTypeDrifted)
			log.FromContext(ctx).V(1).Info("removing drifted status condition, not drifted")
		}
		return reconcile.Result{RequeueAfter: 5 * time.Minute}, nil
	}
	// 3. Finally, if the NodeClaim is drifted, but doesn't have status condition, add it.
	nodeClaim.StatusConditions().SetTrueWithReason(v1.ConditionTypeDrifted, string(driftedReason), string(driftedReason))
	if !hasDriftedCondition {
		log.FromContext(ctx).V(1).WithValues("reason", string(driftedReason)).Info("marking drifted")
	}
	// Requeue after 5 minutes for the cache TTL
	return reconcile.Result{RequeueAfter: 5 * time.Minute}, nil
}
