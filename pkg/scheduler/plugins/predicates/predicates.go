/*
Copyright 2018 The Kubernetes Authors.

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

package predicates

import (
	"context"
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
	utilFeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/features"
	"k8s.io/kubernetes/pkg/scheduler/apis/config"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/feature"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/interpodaffinity"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/nodeaffinity"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/nodeports"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/nodeunschedulable"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/nodevolumelimits"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/podtopologyspread"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/tainttoleration"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/volumezone"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/api/devices/nvidia/gpushare"
	"volcano.sh/volcano/pkg/scheduler/api/devices/nvidia/vgpu"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/util/k8s"
)

const (
	// PluginName indicates name of volcano scheduler plugin.
	PluginName = "predicates"

	// NodeAffinityEnable is the key for enabling Node Affinity Predicates in scheduler configmap
	NodeAffinityEnable = "predicate.NodeAffinityEnable"

	// NodePortsEnable is the key for enabling Node Port Predicates in scheduler configmap
	NodePortsEnable = "predicate.NodePortsEnable"

	// TaintTolerationEnable is the key for enabling Taint Toleration Predicates in scheduler configmap
	TaintTolerationEnable = "predicate.TaintTolerationEnable"

	// PodAffinityEnable is the key for enabling Pod Affinity Predicates in scheduler configmap
	PodAffinityEnable = "predicate.PodAffinityEnable"

	// NodeVolumeLimitsEnable is the key for enabling Node Volume Limits Predicates in scheduler configmap
	NodeVolumeLimitsEnable = "predicate.NodeVolumeLimitsEnable"

	// VolumeZoneEnable is the key for enabling Volume Zone Predicates in scheduler configmap
	VolumeZoneEnable = "predicate.VolumeZoneEnable"

	// PodTopologySpreadEnable is the key for enabling Pod Topology Spread Predicates in scheduler configmap
	PodTopologySpreadEnable = "predicate.PodTopologySpreadEnable"

	// GPUSharingPredicate is the key for enabling GPU Sharing Predicate in YAML
	GPUSharingPredicate = "predicate.GPUSharingEnable"
	NodeLockEnable      = "predicate.NodeLockEnable"
	GPUNumberPredicate  = "predicate.GPUNumberEnable"

	VGPUEnable = "predicate.VGPUEnable"

	// CachePredicate control cache predicate feature
	CachePredicate = "predicate.CacheEnable"

	// ProportionalPredicate is the key for enabling Proportional Predicate in YAML
	ProportionalPredicate = "predicate.ProportionalEnable"
	// ProportionalResource is the key for additional resource key name
	ProportionalResource = "predicate.resources"
	// ProportionalResourcesPrefix is the key prefix for additional resource key name
	ProportionalResourcesPrefix = ProportionalResource + "."
)

type predicatesPlugin struct {
	// Arguments given for the plugin
	pluginArguments framework.Arguments
}

// New return predicate plugin
func New(arguments framework.Arguments) framework.Plugin {
	return &predicatesPlugin{pluginArguments: arguments}
}

func (pp *predicatesPlugin) Name() string {
	return PluginName
}

type baseResource struct {
	CPU    float64
	Memory float64
}

type predicateEnable struct {
	nodeAffinityEnable      bool
	nodePortEnable          bool
	taintTolerationEnable   bool
	podAffinityEnable       bool
	nodeVolumeLimitsEnable  bool
	volumeZoneEnable        bool
	podTopologySpreadEnable bool
	cacheEnable             bool
	proportionalEnable      bool
	proportional            map[v1.ResourceName]baseResource
}

func enablePredicate(args framework.Arguments) predicateEnable {
	/*
	   User Should give predicatesEnable in this format(predicate.GPUSharingEnable).
	   Currently supported only GPUSharing predicate checks.

	   actions: "reclaim, allocate, backfill, preempt"
	   tiers:
	   - plugins:
	     - name: priority
	     - name: gang
	     - name: conformance
	   - plugins:
	     - name: drf
	     - name: predicates
	       arguments:
	         predicate.NodeAffinityEnable: true
	         predicate.NodePortsEnable: true
	         predicate.TaintTolerationEnable: true
	         predicate.PodAffinityEnable: true
	         predicate.NodeVolumeLimitsEnable: true
	         predicate.VolumeZoneEnable: true
	         predicate.PodTopologySpreadEnable: true
	         predicate.GPUSharingEnable: true
	         predicate.GPUNumberEnable: true
	         predicate.CacheEnable: true
	         predicate.ProportionalEnable: true
	         predicate.resources: nvidia.com/gpu
	         predicate.resources.nvidia.com/gpu.cpu: 4
	         predicate.resources.nvidia.com/gpu.memory: 8
	     - name: proportion
	     - name: nodeorder
	*/

	predicate := predicateEnable{
		nodeAffinityEnable:      true,
		nodePortEnable:          true,
		taintTolerationEnable:   true,
		podAffinityEnable:       true,
		nodeVolumeLimitsEnable:  true,
		volumeZoneEnable:        true,
		podTopologySpreadEnable: true,
		cacheEnable:             false,
		proportionalEnable:      false,
	}

	// Checks whether predicate enable args is provided or not.
	// If args were given by scheduler configmap, cover the values in predicateEnable struct.
	args.GetBool(&predicate.nodeAffinityEnable, NodeAffinityEnable)
	args.GetBool(&predicate.nodePortEnable, NodePortsEnable)
	args.GetBool(&predicate.taintTolerationEnable, TaintTolerationEnable)
	args.GetBool(&predicate.podAffinityEnable, PodAffinityEnable)
	args.GetBool(&predicate.nodeVolumeLimitsEnable, NodeVolumeLimitsEnable)
	args.GetBool(&predicate.volumeZoneEnable, VolumeZoneEnable)
	args.GetBool(&predicate.podTopologySpreadEnable, PodTopologySpreadEnable)

	// Checks whether predicate.GPUSharingEnable is provided or not, if given, modifies the value in predicateEnable struct.
	args.GetBool(&gpushare.GpuSharingEnable, GPUSharingPredicate)
	args.GetBool(&gpushare.GpuNumberEnable, GPUNumberPredicate)
	args.GetBool(&gpushare.NodeLockEnable, NodeLockEnable)
	args.GetBool(&vgpu.VGPUEnable, VGPUEnable)

	if gpushare.GpuSharingEnable && gpushare.GpuNumberEnable {
		klog.Fatal("can not define true in both gpu sharing and gpu number")
	}
	if (gpushare.GpuSharingEnable || gpushare.GpuNumberEnable) && vgpu.VGPUEnable {
		klog.Fatal("gpu-share and vgpu can't be used together")
	}
	args.GetBool(&predicate.cacheEnable, CachePredicate)
	// Checks whether predicate.ProportionalEnable is provided or not, if given, modifies the value in predicateEnable struct.
	args.GetBool(&predicate.proportionalEnable, ProportionalPredicate)
	resourcesProportional := make(map[v1.ResourceName]baseResource)
	resourcesStr, ok := args[ProportionalResource].(string)
	if !ok {
		resourcesStr = ""
	}
	resources := strings.Split(resourcesStr, ",")
	for _, resource := range resources {
		resource = strings.TrimSpace(resource)
		if resource == "" {
			continue
		}
		// proportional.resources.[ResourceName]
		cpuResourceKey := ProportionalResourcesPrefix + resource + ".cpu"
		cpuResourceRate := 1.0
		args.GetFloat64(&cpuResourceRate, cpuResourceKey)
		if cpuResourceRate < 0 {
			cpuResourceRate = 1.0
		}
		memoryResourceKey := ProportionalResourcesPrefix + resource + ".memory"
		memoryResourceRate := 1.0
		args.GetFloat64(&memoryResourceRate, memoryResourceKey)
		if memoryResourceRate < 0 {
			memoryResourceRate = 1.0
		}
		r := baseResource{
			CPU:    cpuResourceRate,
			Memory: memoryResourceRate,
		}
		resourcesProportional[v1.ResourceName(resource)] = r
	}
	predicate.proportional = resourcesProportional

	return predicate
}

func (pp *predicatesPlugin) OnSessionOpen(ssn *framework.Session) {
	pl := ssn.PodLister
	nodeMap := ssn.NodeMap

	pCache := predicateCacheNew()
	predicate := enablePredicate(pp.pluginArguments)

	// Register event handlers to update task info in PodLister & nodeMap
	ssn.AddEventHandler(&framework.EventHandler{
		AllocateFunc: func(event *framework.Event) {
			klog.V(4).Infoln("predicates, allocate", event.Task.NodeName)
			pod := pl.UpdateTask(event.Task, event.Task.NodeName)
			nodeName := event.Task.NodeName
			node, found := nodeMap[nodeName]
			if !found {
				klog.Errorf("predicates, update pod %s/%s allocate to NOT EXIST node [%s]", pod.Namespace, pod.Name, nodeName)
				return
			}
			nodeInfo, ok := ssn.Nodes[nodeName]
			if !ok {
				klog.Errorf("Failed to get node %s info from cache", nodeName)
				return
			}
			//predicate gpu sharing
			for _, val := range api.RegisteredDevices {
				if devices, ok := nodeInfo.Others[val].(api.Devices); ok {
					if !devices.HasDeviceRequest(pod) {
						continue
					}

					err := devices.Allocate(ssn.KubeClient(), pod)
					if err != nil {
						klog.Errorf("AllocateToPod failed %s", err.Error())
						return
					}
				} else {
					klog.Warningf("Devices %s assertion conversion failed, skip", val)
				}
			}
			node.AddPod(pod)
			klog.V(4).Infof("predicates, update pod %s/%s allocate to node [%s]", pod.Namespace, pod.Name, nodeName)
		},
		DeallocateFunc: func(event *framework.Event) {
			klog.V(4).Infoln("predicates, deallocate", event.Task.NodeName)
			pod := pl.UpdateTask(event.Task, "")
			nodeName := event.Task.NodeName
			node, found := nodeMap[nodeName]
			if !found {
				klog.Errorf("predicates, update pod %s/%s allocate from NOT EXIST node [%s]", pod.Namespace, pod.Name, nodeName)
				return
			}

			nodeInfo, ok := ssn.Nodes[nodeName]
			if !ok {
				klog.Errorf("Failed to get node %s info from cache", nodeName)
				return
			}

			for _, val := range api.RegisteredDevices {
				if devices, ok := nodeInfo.Others[val].(api.Devices); ok {
					if !devices.HasDeviceRequest(pod) {
						continue
					}

					// deallocate pod gpu id
					err := devices.Release(ssn.KubeClient(), pod)
					if err != nil {
						klog.Errorf(err.Error())
						return
					}
				} else {
					klog.Warningf("Devices %s assertion conversion failed, skip", val)
				}
			}

			err := node.RemovePod(pod)
			if err != nil {
				klog.Errorf("predicates, remove pod %s/%s from node [%s] error: %v", pod.Namespace, pod.Name, nodeName, err)
				return
			}
			klog.V(4).Infof("predicates, update pod %s/%s deallocate from node [%s]", pod.Namespace, pod.Name, nodeName)
		},
	})

	features := feature.Features{
		EnableReadWriteOncePod:                       utilFeature.DefaultFeatureGate.Enabled(features.ReadWriteOncePod),
		EnableVolumeCapacityPriority:                 utilFeature.DefaultFeatureGate.Enabled(features.VolumeCapacityPriority),
		EnableMinDomainsInPodTopologySpread:          utilFeature.DefaultFeatureGate.Enabled(features.MinDomainsInPodTopologySpread),
		EnableNodeInclusionPolicyInPodTopologySpread: utilFeature.DefaultFeatureGate.Enabled(features.NodeInclusionPolicyInPodTopologySpread),
		EnableMatchLabelKeysInPodTopologySpread:      utilFeature.DefaultFeatureGate.Enabled(features.MatchLabelKeysInPodTopologySpread),
	}
	// Initialize k8s plugins
	// TODO: Add more predicates, k8s.io/kubernetes/pkg/scheduler/framework/plugins/legacy_registry.go
	handle := k8s.NewFrameworkHandle(nodeMap, ssn.KubeClient(), ssn.InformerFactory())
	// 1. NodeUnschedulable
	plugin, _ := nodeunschedulable.New(nil, handle)
	nodeUnscheduleFilter := plugin.(*nodeunschedulable.NodeUnschedulable)
	// 2. NodeAffinity
	nodeAffinityArgs := config.NodeAffinityArgs{
		AddedAffinity: &v1.NodeAffinity{},
	}
	plugin, _ = nodeaffinity.New(&nodeAffinityArgs, handle)
	nodeAffinityFilter := plugin.(*nodeaffinity.NodeAffinity)
	// 3. NodePorts
	plugin, _ = nodeports.New(nil, handle)
	nodePortFilter := plugin.(*nodeports.NodePorts)
	// 4. TaintToleration
	plugin, _ = tainttoleration.New(nil, handle)
	tolerationFilter := plugin.(*tainttoleration.TaintToleration)
	// 5. InterPodAffinity
	plArgs := &config.InterPodAffinityArgs{}
	plugin, _ = interpodaffinity.New(plArgs, handle)
	podAffinityFilter := plugin.(*interpodaffinity.InterPodAffinity)
	// 6. NodeVolumeLimits
	plugin, _ = nodevolumelimits.NewCSI(nil, handle, features)
	nodeVolumeLimitsCSIFilter := plugin.(*nodevolumelimits.CSILimits)
	// 7. VolumeZone
	plugin, _ = volumezone.New(nil, handle)
	volumeZoneFilter := plugin.(*volumezone.VolumeZone)
	// 8. PodTopologySpread
	// Setting cluster level default constraints is not support for now.
	ptsArgs := &config.PodTopologySpreadArgs{DefaultingType: config.SystemDefaulting}
	plugin, _ = podtopologyspread.New(ptsArgs, handle, features)
	podTopologySpreadFilter := plugin.(*podtopologyspread.PodTopologySpread)

	state := k8sframework.NewCycleState()

	ssn.AddPrePredicateFn(pp.Name(), func(task *api.TaskInfo) error {
		// Check NodePorts
		if predicate.nodePortEnable {
			_, status := nodePortFilter.PreFilter(context.TODO(), state, task.Pod)
			if !status.IsSuccess() {
				return fmt.Errorf("plugin %s pre-predicates failed %s", interpodaffinity.Name, status.Message())
			}
		}

		// InterPodAffinity Predicate
		// TODO: Update the node information to be processed by the filer based on the node list returned by the prefilter.
		// In K8S V1.25, the return value result is added to the Prefile interface,
		// indicating the list of nodes that meet filtering conditions.
		// If the value of result is nil, all nodes meet the conditions.
		// If the specified node information exists, only the node information in result meets the conditions.
		// The value of Prefile in the current InterPodAffinity package always returns nil.
		// The outer layer does not need to be processed temporarily.
		// If the filtering logic is added to the Prefile node in the Volumebinding package in the future,
		// the processing logic needs to be added to the return value result.
		if predicate.podAffinityEnable {
			_, status := podAffinityFilter.PreFilter(context.TODO(), state, task.Pod)
			if !status.IsSuccess() {
				return fmt.Errorf("plugin %s pre-predicates failed %s", interpodaffinity.Name, status.Message())
			}
		}

		// Check PodTopologySpread
		// TODO: Update the node information to be processed by the filer based on the node list returned by the prefilter.
		// In K8S V1.25, the return value result is added to the Prefile interface,
		// indicating the list of nodes that meet filtering conditions.
		// If the value of result is nil, all nodes meet the conditions.
		// If the specified node information exists, only the node information in result meets the conditions.
		// The value of Prefile in the current PodTopologySpread package always returns nil.
		// The outer layer does not need to be processed temporarily.
		// If the filtering logic is added to the Prefile node in the Volumebinding package in the future,
		// the processing logic needs to be added to the return value result.
		if predicate.podTopologySpreadEnable {
			_, status := podTopologySpreadFilter.PreFilter(context.TODO(), state, task.Pod)
			if !status.IsSuccess() {
				return fmt.Errorf("plugin %s pre-predicates failed %s", podTopologySpreadFilter.Name(), status.Message())
			}
		}
		return nil
	})

	ssn.AddPredicateFn(pp.Name(), func(task *api.TaskInfo, node *api.NodeInfo) ([]*api.Status, error) {
		predicateStatus := make([]*api.Status, 0)
		nodeInfo, found := nodeMap[node.Name]
		if !found {
			return predicateStatus, fmt.Errorf("failed to predicates, node info for %s not found", node.Name)
		}

		if node.Allocatable.MaxTaskNum <= len(nodeInfo.Pods) {
			klog.V(4).Infof("NodePodNumber predicates Task <%s/%s> on Node <%s> failed",
				task.Namespace, task.Name, node.Name)
			podsNumStatus := &api.Status{
				// TODO(wangyang0616): When the number of pods of a node reaches the upper limit, preemption is not supported for now.
				// Record details in #3079 (volcano.sh/volcano)
				// In the preempt stage, the pipeline of the pod number is not considered,
				// the preemption of the pod number is released directly, which will cause the pods in the node to be cyclically evicted.
				Code:   api.UnschedulableAndUnresolvable,
				Reason: api.NodePodNumberExceeded,
			}
			predicateStatus = append(predicateStatus, podsNumStatus)
			return predicateStatus, fmt.Errorf("%s", api.NodePodNumberExceeded)
		}

		predicateByStablefilter := func(pod *v1.Pod, nodeInfo *k8sframework.NodeInfo) ([]*api.Status, bool, error) {
			// CheckNodeUnschedulable
			predicateStatus := make([]*api.Status, 0)
			status := nodeUnscheduleFilter.Filter(context.TODO(), state, task.Pod, nodeInfo)
			nodeUnscheduleStatus := framework.ConvertPredicateStatus(status)
			if nodeUnscheduleStatus.Code != api.Success {
				predicateStatus = append(predicateStatus, nodeUnscheduleStatus)
				return predicateStatus, false, fmt.Errorf("plugin %s predicates failed %s", nodeUnscheduleFilter.Name(), status.Message())
			}

			// Check NodeAffinity
			if predicate.nodeAffinityEnable {
				status := nodeAffinityFilter.Filter(context.TODO(), state, task.Pod, nodeInfo)
				nodeAffinityStatus := framework.ConvertPredicateStatus(status)
				if nodeAffinityStatus.Code != api.Success {
					predicateStatus = append(predicateStatus, nodeAffinityStatus)
					return predicateStatus, false, fmt.Errorf("plugin %s predicates failed %s", nodeAffinityFilter.Name(), status.Message())
				}
			}

			// PodToleratesNodeTaints: TaintToleration
			if predicate.taintTolerationEnable {
				status := tolerationFilter.Filter(context.TODO(), state, task.Pod, nodeInfo)
				tolerationStatus := framework.ConvertPredicateStatus(status)
				if tolerationStatus.Code != api.Success {
					predicateStatus = append(predicateStatus, tolerationStatus)
					return predicateStatus, false, fmt.Errorf("plugin %s predicates failed %s", tolerationFilter.Name(), status.Message())
				}
			}

			return predicateStatus, true, nil
		}

		// Check PredicateWithCache
		var err error
		var fit bool
		predicateCacheStatus := make([]*api.Status, 0)
		if predicate.cacheEnable {
			fit, err = pCache.PredicateWithCache(node.Name, task.Pod)
			if err != nil {
				predicateCacheStatus, fit, err = predicateByStablefilter(task.Pod, nodeInfo)
				pCache.UpdateCache(node.Name, task.Pod, fit)
			} else {
				if !fit {
					err = fmt.Errorf("plugin equivalence cache predicates failed")
				}
			}
		} else {
			predicateCacheStatus, fit, err = predicateByStablefilter(task.Pod, nodeInfo)
		}

		predicateStatus = append(predicateStatus, predicateCacheStatus...)
		if !fit {
			return predicateStatus, err
		}

		// Check NodePort
		if predicate.nodePortEnable {
			status := nodePortFilter.Filter(context.TODO(), state, nil, nodeInfo)
			nodePortStatus := framework.ConvertPredicateStatus(status)
			if nodePortStatus.Code != api.Success {
				predicateStatus = append(predicateStatus, nodePortStatus)
				return predicateStatus, fmt.Errorf("plugin %s predicates failed %s", nodePortFilter.Name(), status.Message())
			}
		}

		// Check PodAffinity
		if predicate.podAffinityEnable {
			status := podAffinityFilter.Filter(context.TODO(), state, task.Pod, nodeInfo)
			podAffinityStatus := framework.ConvertPredicateStatus(status)
			if podAffinityStatus.Code != api.Success {
				predicateStatus = append(predicateStatus, podAffinityStatus)
				return predicateStatus, fmt.Errorf("plugin %s predicates failed %s", podAffinityFilter.Name(), status.Message())
			}
		}

		// Check NodeVolumeLimits
		if predicate.nodeVolumeLimitsEnable {
			status := nodeVolumeLimitsCSIFilter.Filter(context.TODO(), state, task.Pod, nodeInfo)
			nodeVolumeStatus := framework.ConvertPredicateStatus(status)
			if nodeVolumeStatus.Code != api.Success {
				predicateStatus = append(predicateStatus, nodeVolumeStatus)
				return predicateStatus, fmt.Errorf("plugin %s predicates failed %s", nodeVolumeLimitsCSIFilter.Name(), status.Message())
			}
		}

		// Check VolumeZone
		if predicate.volumeZoneEnable {
			status := volumeZoneFilter.Filter(context.TODO(), state, task.Pod, nodeInfo)
			volumeZoneStatus := framework.ConvertPredicateStatus(status)
			if volumeZoneStatus.Code != api.Success {
				predicateStatus = append(predicateStatus, volumeZoneStatus)
				return predicateStatus, fmt.Errorf("plugin %s predicates failed %s", volumeZoneFilter.Name(), status.Message())
			}
		}

		// Check PodTopologySpread
		if predicate.podTopologySpreadEnable {
			status := podTopologySpreadFilter.Filter(context.TODO(), state, task.Pod, nodeInfo)
			podTopologyStatus := framework.ConvertPredicateStatus(status)
			if podTopologyStatus.Code != api.Success {
				predicateStatus = append(predicateStatus, podTopologyStatus)
				return predicateStatus, fmt.Errorf("plugin %s predicates failed %s", podTopologySpreadFilter.Name(), status.Message())
			}
		}

		for _, val := range api.RegisteredDevices {
			if devices, ok := node.Others[val].(api.Devices); ok {
				code, msg, err := devices.FilterNode(task.Pod)
				filterNodeStatus := &api.Status{
					Code:   code,
					Reason: msg,
				}
				if err != nil {
					return predicateStatus, err
				}
				if filterNodeStatus.Code != api.Success {
					predicateStatus = append(predicateStatus, filterNodeStatus)
					return predicateStatus, fmt.Errorf("plugin device filternode predicates failed %s", msg)
				}
			} else {
				klog.Warningf("Devices %s assertion conversion failed, skip", val)
			}
		}

		klog.V(4).Infof("checkNodeGPUPredicate predicates Task <%s/%s> on Node <%s>: fit %v",
			task.Namespace, task.Name, node.Name, fit)

		if predicate.proportionalEnable {
			// Check ProportionalPredicate
			proportionalStatus, err := checkNodeResourceIsProportional(task, node, predicate.proportional)
			if proportionalStatus.Code != api.Success {
				predicateStatus = append(predicateStatus, proportionalStatus)
				return predicateStatus, err
			}
			klog.V(4).Infof("checkNodeResourceIsProportional predicates Task <%s/%s> on Node <%s>: fit %v",
				task.Namespace, task.Name, node.Name, fit)
		}
		return predicateStatus, nil
	})
}

func (pp *predicatesPlugin) OnSessionClose(ssn *framework.Session) {}
