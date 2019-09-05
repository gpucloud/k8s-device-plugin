package main

import (
	"github.com/NVIDIA/gpu-monitoring-tools/bindings/go/nvml"
	"github.com/gpucloud/gohwloc/topology"
	"k8s.io/klog"
)

type pciDevice struct {
	pciType      string
	maxDevices   int
	availDevices int
	score        float64
	nvidiaUUID   string
	children     []*pciDevice
	dev          *topology.HwlocObject
}

// Destroy destroy the device plugin's topology object
func (dp *NvidiaDevicePlugin) Destroy() {
	if dp.topo != nil {
		dp.topo.Destroy()
	}
}

func (dp *NvidiaDevicePlugin) buildPciDeviceTree() error {
	dp.root = &pciDevice{
		pciType: "root",
	}
	t, err := topology.NewTopology()
	if err != nil {
		return err
	}
	t.Load()
	n, err := t.GetNbobjsByType(topology.HwlocObjPackage)
	if err != nil {
		return err
	}
	dp.root.children = make([]*pciDevice, n)
	for i := 0; i < n; i++ {
		nno, err := t.GetObjByType(topology.HwlocObjPackage, uint(i))
		if err != nil {
			klog.Warningf("topology get object by type error: %v", err)
			continue
		}
		dp.root.children[i] = &pciDevice{pciType: nno.Type.String(), dev: nno}
		buildTree(dp.root.children[i], nno)
	}

	return nil
}

func buildTree(node *pciDevice, dev *topology.HwlocObject) {
	if dev == nil {
		return
	}
	if node == nil {
		node = &pciDevice{
			pciType: dev.Type.String(),
			dev:     dev,
		}
	}
	node.children = make([]*pciDevice, len(dev.Children))
	for i := 0; i < len(dev.Children); i++ {
		node.children[i] = &pciDevice{
			pciType: dev.Children[i].Type.String(),
			dev:     dev.Children[i],
		}
		buildTree(node.children[i], dev.Children[i])
	}
}

func (dp *NvidiaDevicePlugin) updateTree(node *pciDevice, init bool) (maxDevices, availDevices int) {
	if node == nil {
		return 0, 0
	}
	if node.dev != nil && node.dev.Attributes.OSDevType == topology.HwlocObjOSDevGPU {
		maxDevices = 1
		if init == false {
			availDevices = node.availDevices
		} else {
			availDevices = 1
		}
		node.maxDevices = maxDevices
		node.availDevices = availDevices
		node.score = 1.0
		node.nvidiaUUID, _ = node.dev.GetInfo("NVIDIAUUID")
	}
	for i := 0; i < len(node.children); i++ {
		tmpMax, tmpAvail := dp.updateTree(node.children[i], init)
		maxDevices += tmpMax
		availDevices += tmpAvail
		node.maxDevices = maxDevices
		node.availDevices = availDevices
		node.score = dp.getAverageScore(node)
	}
	return maxDevices, availDevices
}

func printDeviceTree(node *pciDevice) {
	if node == nil {
		return
	}
	if node.dev != nil {
		backend, _ := node.dev.GetInfo("Backend")
		gpuid := node.nvidiaUUID
		klog.Infof("%v, %v, %v, %v, %v, %#v, %v\n", node.pciType, node.dev.Name, backend, gpuid, node.dev.Attributes.OSDevType, node.availDevices, node.score)
	}
	for i := 0; i < len(node.children); i++ {
		printDeviceTree(node.children[i])
	}
}

func (dp *NvidiaDevicePlugin) findBestDevice(t string, n int) []string {
	devs := []string{}
	switch t {
	case resourceName:
		// XXX: we divide the user's request into two parts:
		// a. request 1 GPU card, select the best 1 GPU card, make sure the left GPU cards will be most valuable
		// b. request more than 1 GPU card, based on the score of the least enough leaves branch
		if n == 1 {
			// request 1 GPU card, select the best 1 GPU card,
			// make sure the left GPU cards will be most valuable
			devs = append(devs, find1GPUDevice(dp.root))
		} else {
			// find the least enough leaves node
			// find the higher score when the two nodes have same number leaves
			// add the leaves into the result
			devs = append(devs, findNGPUDevice(dp.root, n)...)
		}
		return devs
	}

	return devs
}

func find1GPUDevice(root *pciDevice) string {
	// if the current node has maximum GPU devices, select the first one
	// else find the one to make sure left GPU devices have highest score
	// XXX: consider GPU connect type
	if root == nil {
		return ""
	}
	if root.dev.Attributes.OSDevType == topology.HwlocObjOSDevGPU {
		return root.nvidiaUUID
	}
	var min = float64(1 << 10)
	var minDev *pciDevice
	for _, child := range root.children {
		if child.availDevices == 0 {
			continue
		}
		if child.score < min {
			min = child.score
			minDev = child
		}
	}
	return find1GPUDevice(minDev)
}

func findNGPUDevice(root *pciDevice, n int) []string {
	var max float64
	var queue = []*pciDevice{root}
	var tmp = []*pciDevice{}
	for len(queue) > 0 {
		l := len(queue)
		max = 0
		for i := 0; i < l; i++ {
			if queue[i].availDevices < n {
				continue
			}
			if queue[i].score > max {
				max = queue[i].score
			}
		}
		if max == 0 {
			break
		} else {
			tmp = []*pciDevice{}
		}
		for i := 0; i < l; i++ {
			if queue[i].score < max {
				continue
			}
			if queue[i].score == max {
				tmp = append(tmp, queue[i])
			}
			for _, c := range queue[i].children {
				if c.availDevices == 0 {
					continue
				}
				queue = append(queue, c)
			}
		}
		queue = queue[l:]
	}
	var res = []string{}
	for _, pci := range tmp {
		res = append(res, pci.getAvailableGPUs()...)
		if len(res) == n {
			break
		}
	}
	return res
}

func (p *pciDevice) getAvailableGPUs() []string {
	var res = []string{}
	var queue = []*pciDevice{p}
	for len(queue) > 0 {
		l := len(queue)
		for i := 0; i < l; i++ {
			if queue[i].dev != nil && queue[i].dev.Attributes.OSDevType == topology.HwlocObjOSDevGPU {
				if queue[i].availDevices == 1 {
					res = append(res, queue[i].nvidiaUUID)
				}
			} else if queue[i].availDevices > 0 {
				for _, c := range queue[i].children {
					if c.availDevices == 0 {
						continue
					}
					queue = append(queue, c)
				}
			}
		}
		queue = queue[l:]
	}
	return res
}

func (dp *NvidiaDevicePlugin) getAverageScore(root *pciDevice) float64 {
	if root == nil {
		return 0
	}
	gpus := root.getAvailableGPUs()
	if len(gpus) == 1 {
		return 0
	}
	gpuMap := map[string]*nvml.Device{}
	for i := range dp.devs {
		gpuMap[dp.devs[i].UUID] = dp.devs[i]
	}

	var score float64
	for i, uuid := range gpus {
		for j := i + 1; j < len(gpus); j++ {
			p2pType, err := nvml.GetP2PLink(gpuMap[uuid], gpuMap[gpus[j]])
			check(err)
			score += float64(makeLinkScore(p2pType))
		}
	}
	return score / float64(len(gpus))
}

// UpdatePodDevice update the pod device with added or deleted GPU UUID
func (dp *NvidiaDevicePlugin) UpdatePodDevice(adds, dels []string) error {
	var allocatedMap = map[string]int{}
	for _, s := range adds {
		allocatedMap[s] = 0
	}
	for _, s := range dels {
		allocatedMap[s] = 1
	}
	updateDeviceState(dp.root, allocatedMap)
	dp.updateTree(dp.root, false)
	klog.Infof("Update Pod Device: left available devices number: %v", dp.root.availDevices)
	return nil
}

func updateDeviceState(root *pciDevice, m map[string]int) {
	if root == nil {
		return
	}
	if root.nvidiaUUID != "" {
		//klog.Infof("find %v availDevices", root.nvidiaUUID)
		if val, ok := m[root.nvidiaUUID]; ok {
			root.availDevices = val
			//klog.Infof("Set %v availDevices to %v", root.nvidiaUUID, val)
		}
		return
	}
	for i := 0; i < len(root.children); i++ {
		updateDeviceState(root.children[i], m)
	}
}
