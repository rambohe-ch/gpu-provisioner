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
	"fmt"

	"github.com/awslabs/operatorpkg/status"
	"github.com/azure/gpu-provisioner/pkg/providers/instance"
	"github.com/samber/lo"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"
	"sigs.k8s.io/karpenter/pkg/scheduling"
)

// func init() {
// 	coreapis.Settings = append(coreapis.Settings, apis.Settings...)
// }

const (
	InstanceTypeDrift cloudprovider.DriftReason = "InstanceTypeDrift"
)

var _ cloudprovider.CloudProvider = &CloudProvider{}

type CloudProvider struct {
	instanceProvider *instance.Provider
	kubeClient       client.Client
}

func New(instanceProvider *instance.Provider, kubeClient client.Client) *CloudProvider {
	return &CloudProvider{
		instanceProvider: instanceProvider,
		kubeClient:       kubeClient,
	}
}

// Create a node given the constraints.
func (c *CloudProvider) Create(ctx context.Context, nodeClaim *karpenterv1.NodeClaim) (*karpenterv1.NodeClaim, error) {
	klog.InfoS("Create", "nodeClaim", klog.KObj(nodeClaim))

	instance, err := c.instanceProvider.Create(ctx, nodeClaim)
	if err != nil {
		return nil, fmt.Errorf("creating instance, %w", err)
	}
	nc := c.instanceToNodeClaim(ctx, instance)
	nc.Labels = lo.Assign(nc.Labels, instance.Labels)
	return nc, nil
}

func (c *CloudProvider) List(ctx context.Context) ([]*karpenterv1.NodeClaim, error) {
	nodeClaims := []*karpenterv1.NodeClaim{}
	instances, err := c.instanceProvider.List(ctx)
	if err != nil {
		return nil, err
	}

	for index := range instances {
		nodeClaims = append(nodeClaims, c.instanceToNodeClaim(ctx, instances[index]))
	}
	return nodeClaims, nil
}

func (c *CloudProvider) Get(ctx context.Context, providerID string) (*karpenterv1.NodeClaim, error) {
	klog.InfoS("Get", "providerID", providerID)

	instance, err := c.instanceProvider.Get(ctx, providerID)
	if err != nil {
		return nil, fmt.Errorf("getting instance , %w", err)
	}
	if instance == nil {
		return nil, fmt.Errorf("cannot find a ready instance , %w", err)
	}
	return c.instanceToNodeClaim(ctx, instance), err
}

func (c *CloudProvider) Delete(ctx context.Context, nodeClaim *karpenterv1.NodeClaim) error {
	klog.InfoS("Delete", "nodeClaim", klog.KObj(nodeClaim))
	return c.instanceProvider.Delete(ctx, nodeClaim.Status.ProviderID)
}

func (c *CloudProvider) IsDrifted(ctx context.Context, nodeClaim *karpenterv1.NodeClaim) (cloudprovider.DriftReason, error) {
	klog.InfoS("IsDrifted", "nodeclaim", klog.KObj(nodeClaim))

	// check instance type is drifted or not
	instanceTypes := scheduling.NewNodeSelectorRequirementsWithMinValues(nodeClaim.Spec.Requirements...).Get("node.kubernetes.io/instance-type").Values()
	if len(instanceTypes) == 0 {
		return cloudprovider.DriftReason(""), fmt.Errorf("nodeClaim spec has no requirement for instance type")
	}

	instance, err := c.instanceProvider.Get(ctx, nodeClaim.Status.ProviderID)
	if err != nil {
		return "", err
	}

	// instance type is drifted
	if *instance.Type != instanceTypes[0] {
		return InstanceTypeDrift, nil
	}

	return cloudprovider.DriftReason(""), nil
}

func (c *CloudProvider) GetInstanceTypes(ctx context.Context, nodePool *karpenterv1.NodePool) ([]*cloudprovider.InstanceType, error) {
	return []*cloudprovider.InstanceType{}, nil
}

// Name returns the CloudProvider implementation name.
func (c *CloudProvider) Name() string {
	return "azure"
}

func (c *CloudProvider) GetSupportedNodeClasses() []status.Object {
	return []status.Object{}
}
