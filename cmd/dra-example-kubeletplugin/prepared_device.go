package main

import (
	drapbv1 "k8s.io/kubelet/pkg/apis/dra/v1beta1"
	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"
)

type PreparedDevice struct {
	drapbv1.Device
	ContainerEdits *cdiapi.ContainerEdits
}

type PreparedDevices []*PreparedDevice

func (pds PreparedDevices) GetDevices() []*drapbv1.Device {
	var devices []*drapbv1.Device
	for _, pd := range pds {
		devices = append(devices, &pd.Device)
	}
	return devices
}
