/*
       Copyright (c) Microsoft Corporation.
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

package cloudprovider

import (
	"context"

	"github.com/azure/gpu-provisioner/pkg/providers/instance"
	"github.com/samber/lo"
	"knative.dev/pkg/logging"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

func (c *CloudProvider) instanceToNodeClaim(ctx context.Context, instanceObj *instance.Instance) *karpenterv1.NodeClaim {
	nodeClaim := &karpenterv1.NodeClaim{}
	if instanceObj == nil {
		return nodeClaim
	}

	labels := instanceObj.Labels
	annotations := map[string]string{}

	nodeClaim.Name = lo.FromPtr(instanceObj.Name)

	if instanceObj.CapacityType != nil {
		labels[karpenterv1.CapacityTypeLabelKey] = *instanceObj.CapacityType
	}

	if instanceObj != nil && instanceObj.Tags[karpenterv1.NodePoolLabelKey] != nil {
		labels[karpenterv1.NodePoolLabelKey] = *instanceObj.Tags[karpenterv1.NodePoolLabelKey]
	}

	if instanceObj != nil && instanceObj.Tags[karpenterv1.NodeClassReferenceAnnotationKey] != nil {
		annotations[karpenterv1.NodeClassReferenceAnnotationKey] = *instanceObj.Tags[karpenterv1.NodeClassReferenceAnnotationKey]
	}

	nodeClaim.Labels = labels
	nodeClaim.Annotations = annotations

	if instanceObj != nil && instanceObj.ID != nil {
		nodeClaim.Status.ProviderID = lo.FromPtr(instanceObj.ID)
		annotations[karpenterv1.NodeClassReferenceAnnotationKey] = lo.FromPtr(instanceObj.ID)
	} else {
		logging.FromContext(ctx).Warnf("Provider ID cannot be nil")
	}
	nodeClaim.Status.ImageID = *instanceObj.ImageID

	return nodeClaim
}
