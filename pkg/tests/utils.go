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

package tests

import (
	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v4"
	"github.com/samber/lo"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

func GetNodeClaimObj(name string, labels map[string]string, taints []v1.Taint, resource karpenterv1.ResourceRequirements, req []v1.NodeSelectorRequirement) *karpenterv1.NodeClaim {
	requirements := lo.Map(req, func(v1Requirements v1.NodeSelectorRequirement, _ int) karpenterv1.NodeSelectorRequirementWithMinValues {
		return karpenterv1.NodeSelectorRequirementWithMinValues{
			NodeSelectorRequirement: v1.NodeSelectorRequirement{
				Key:      v1Requirements.Key,
				Operator: v1Requirements.Operator,
				Values:   v1Requirements.Values,
			},
			MinValues: to.Ptr(int(1)),
		}
	})
	return &karpenterv1.NodeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "nodeclaim-ns",
			Labels:    labels,
		},
		Spec: karpenterv1.NodeClaimSpec{
			Resources:    resource,
			Requirements: requirements,
			NodeClassRef: &karpenterv1.NodeClassReference{},
			Taints:       taints,
		},
	}
}

func GetAgentPoolObj(apType armcontainerservice.AgentPoolType, capacityType armcontainerservice.ScaleSetPriority,
	labels map[string]*string, taints []*string, storage int32, vmSize string) armcontainerservice.AgentPool {
	return armcontainerservice.AgentPool{
		Properties: &armcontainerservice.ManagedClusterAgentPoolProfileProperties{
			NodeLabels:       labels,
			NodeTaints:       taints,
			Type:             to.Ptr(apType),
			VMSize:           to.Ptr(vmSize),
			OSType:           to.Ptr(armcontainerservice.OSTypeLinux),
			Count:            to.Ptr(int32(1)),
			ScaleSetPriority: to.Ptr(capacityType),
			OSDiskSizeGB:     to.Ptr(storage),
		},
	}
}

func GetAgentPoolObjWithName(apName string, apId string, vmSize string) armcontainerservice.AgentPool {
	return armcontainerservice.AgentPool{
		Name: &apName,
		ID:   &apId,
		Properties: &armcontainerservice.ManagedClusterAgentPoolProfileProperties{
			VMSize: &vmSize,
			NodeLabels: map[string]*string{
				"test": to.Ptr("test"),
			},
		},
	}
}
func GetNodeList(nodes []v1.Node) *v1.NodeList {
	return &v1.NodeList{
		Items: nodes,
	}
}

var (
	ReadyNode = v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "aks-agentpool0-20562481-vmss_0",
			Labels: map[string]string{
				"agentpool":                      "agentpool0",
				"kubernetes.azure.com/agentpool": "agentpool0",
			},
		},
		Spec: v1.NodeSpec{
			ProviderID: "azure:///subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/nodeRG/providers/Microsoft.Compute/virtualMachineScaleSets/aks-agentpool0-20562481-vmss/virtualMachines/0",
		},
		Status: v1.NodeStatus{
			Conditions: []v1.NodeCondition{
				{
					Type:   v1.NodeReady,
					Status: v1.ConditionTrue,
				},
			},
		},
	}
)

func NotFoundAzError() *azcore.ResponseError {
	return &azcore.ResponseError{ErrorCode: "NotFound"}
}
