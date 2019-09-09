package main

import (
	"github.com/NVIDIA/gpu-monitoring-tools/bindings/go/nvml"
	v1 "k8s.io/api/core/v1"
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

var linkScoreTable = map[nvml.P2PLinkType]int{
	nvml.P2PLinkCrossCPU:     1,
	nvml.P2PLinkSameCPU:      2,
	nvml.P2PLinkHostBridge:   3,
	nvml.P2PLinkMultiSwitch:  4,
	nvml.P2PLinkSingleSwitch: 5,
	nvml.P2PLinkSameBoard:    6,
	nvml.SingleNVLINKLink:    4,
	nvml.TwoNVLINKLinks:      5,
	nvml.ThreeNVLINKLinks:    6,
	nvml.FourNVLINKLinks:     7,
	nvml.FiveNVLINKLinks:     8,
	nvml.SixNVLINKLinks:      9,
	nvml.P2PLinkUnknown:      0,
}

func makeLinkScore(t nvml.P2PLinkType) int {
	score, exists := linkScoreTable[t]
	if !exists {
		score = 0
	}
	return score
}
