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

package lifecycle

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	v1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"
	"sigs.k8s.io/karpenter/pkg/events"
)

func InsufficientCapacityErrorEvent(nodeClaim *v1.NodeClaim, err error) events.Event {
	return events.Event{
		InvolvedObject: nodeClaim,
		Type:           corev1.EventTypeWarning,
		Reason:         "InsufficientCapacityError",
		Message:        fmt.Sprintf("NodeClaim %s event: %s", nodeClaim.Name, truncateMessage(err.Error())),
		DedupeValues:   []string{string(nodeClaim.UID)},
	}
}

func NodeClassNotReadyEvent(nodeClaim *v1.NodeClaim, err error) events.Event {
	return events.Event{
		InvolvedObject: nodeClaim,
		Type:           corev1.EventTypeWarning,
		Reason:         "NodeClassNotReady",
		Message:        fmt.Sprintf("NodeClaim %s event: %s", nodeClaim.Name, truncateMessage(err.Error())),
		DedupeValues:   []string{string(nodeClaim.UID)},
	}
}

func NodeClaimErrorEventAndReasonLabel(nodeClaim *v1.NodeClaim, err error) (events.Event, string) {
	var reason, reasonLabel string

	switch {
	case cloudprovider.IsInsufficientCapacityError(err):
		reason, reasonLabel = "InsufficientCapacityError", "insufficient_capacity"
	case cloudprovider.IsLocationRestrictionError(err):
		reason, reasonLabel = "LocationRestrictionError", "location_restriction"
	case cloudprovider.IsInvalidParameterError(err):
		reason, reasonLabel = "InvalidParameterError", "invalid_parameter"
	default:
		reason, reasonLabel = "InsufficientCapacityError", "insufficient_capacity"
	}

	return events.Event{
		InvolvedObject: nodeClaim,
		Type:           corev1.EventTypeWarning,
		Reason:         reason,
		Message:        fmt.Sprintf("NodeClaim %s event: %s", nodeClaim.Name, truncateMessage(err.Error())),
		DedupeValues:   []string{string(nodeClaim.UID)},
	}, reasonLabel
}
