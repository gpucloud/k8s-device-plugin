package main

import (
	"github.com/NVIDIA/gpu-monitoring-tools/bindings/go/nvml"
	"k8s.io/api/core/v1"
	schedulernodeinfo "k8s.io/kubernetes/pkg/scheduler/nodeinfo"
)

// IsGPUTopoPod determines if it's the pod for GPU topology
func IsGPUTopoPod(pod *v1.Pod) bool {
	return GetGPUTopoNum(pod) > 0
}

func GetGPUTopoNum(pod *v1.Pod) int64 {

	res := &schedulernodeinfo.Resource{}
	for _, container := range pod.Spec.Containers {
		res.Add(container.Resources.Requests)
	}

	// take max_resource(sum_pod, any_init_container)
	for _, container := range pod.Spec.InitContainers {
		res.SetMaxResource(container.Resources.Requests)
	}

	resList := res.ResourceList()
	gpuTopo := resList["nvidia.com/gpu-topo"]
	gpuTopoNum, _ := gpuTopo.AsInt64()

	return gpuTopoNum
}

func makeLinkScore(t nvml.P2PLinkType) int {
	switch t {
	case nvml.P2PLinkCrossCPU:
		return 1
	case nvml.P2PLinkSameCPU:
		return 2
	case nvml.P2PLinkHostBridge:
		return 3
	case nvml.P2PLinkMultiSwitch:
		return 4
	case nvml.P2PLinkSingleSwitch:
		return 5
	case nvml.P2PLinkSameBoard:
		return 6
	case nvml.SingleNVLINKLink:
		return 4
	case nvml.TwoNVLINKLinks:
		return 5
	case nvml.ThreeNVLINKLinks:
		return 6
	case nvml.FourNVLINKLinks:
		return 7
	case nvml.FiveNVLINKLinks:
		return 8
	case nvml.SixNVLINKLinks:
		return 9
	case nvml.P2PLinkUnknown:
	}
	return 0
}
