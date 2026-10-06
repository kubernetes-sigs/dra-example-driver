/*
 * Copyright The Kubernetes Authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	coreclientset "k8s.io/client-go/kubernetes"
	"k8s.io/dynamic-resource-allocation/resourceslice"
	drapbv1 "k8s.io/kubelet/pkg/apis/dra/v1beta1"
	"k8s.io/kubernetes/pkg/kubelet/checkpointmanager"

	nicconfig "github.com/soer3n/kube-ovn-dra-driver/api/kube-ovn.io/resource/nic/v1alpha1"
	"github.com/soer3n/kube-ovn-dra-driver/internal/profiles"
	nicprofile "github.com/soer3n/kube-ovn-dra-driver/internal/profiles/nic"
	"github.com/soer3n/kube-ovn-dra-driver/pkg/nicprepare"
	"github.com/soer3n/kube-ovn-dra-driver/pkg/plumbing"
)

type AllocatableDevices map[string]resourceapi.Device
type PreparedClaims map[string]profiles.PreparedDevices

// maxPrepareConcurrency bounds how many NICs of a claim wait for their
// kube-ovn allocation at once during PrepareResourceClaims, so prepare latency
// stays ~flat in NIC count without flooding the API server with pod reads.
const maxPrepareConcurrency = 16

type OpaqueDeviceConfig struct {
	Requests []string
	Config   runtime.Object
}

type DeviceState struct {
	sync.Mutex
	driverName        string
	cdi               *CDIHandler
	driverResources   resourceslice.DriverResources
	allocatable       AllocatableDevices
	checkpointManager checkpointmanager.CheckpointManager
	configDecoder     runtime.Decoder
	configHandler     profiles.ConfigHandler
	profile           profiles.Profile
	nodeName          string
	// NIC-specific: set when the profile is nicprofile.ProfileName
	coreclient coreclientset.Interface
	isNIC      bool
	// nicStore receives one plumbing.Spec per claimed NIC, so the NRI sandbox
	// hooks can attach it once the netns exists.
	nicStore *plumbing.NICStore
}

func NewDeviceState(config *Config) (*DeviceState, error) {
	driverResources, err := config.profile.EnumerateDevices()
	if err != nil {
		return nil, fmt.Errorf("error enumerating all possible devices: %v", err)
	}

	cdi, err := NewCDIHandler(config.flags.cdiRoot, config.flags.driverName, config.flags.profile)
	if err != nil {
		return nil, fmt.Errorf("unable to create CDI handler: %v", err)
	}

	err = cdi.CreateCommonSpecFile()
	if err != nil {
		return nil, fmt.Errorf("unable to create CDI spec file for common edits: %v", err)
	}

	checkpointManager, err := checkpointmanager.NewCheckpointManager(config.DriverPluginPath())
	if err != nil {
		return nil, fmt.Errorf("unable to create checkpoint manager: %v", err)
	}

	configScheme := runtime.NewScheme()
	configHandler := config.profile
	sb := configHandler.SchemeBuilder()
	if err := sb.AddToScheme(configScheme); err != nil {
		return nil, fmt.Errorf("create config scheme: %w", err)
	}

	// Set up a json serializer to decode our types.
	decoder := json.NewSerializerWithOptions(
		json.DefaultMetaFactory,
		configScheme,
		configScheme,
		json.SerializerOptions{
			Pretty: true, Strict: true,
		},
	)

	allocatable := allocatableDevices(driverResources, config.flags.nodeName)

	state := &DeviceState{
		driverName:        config.flags.driverName,
		cdi:               cdi,
		driverResources:   driverResources,
		allocatable:       allocatable,
		checkpointManager: checkpointManager,
		configDecoder:     decoder,
		configHandler:     configHandler,
		profile:           config.profile,
		nodeName:          config.flags.nodeName,
		coreclient:        config.coreclient,
		isNIC:             config.flags.profile == nicprofile.ProfileName,
		nicStore:          config.nicStore,
	}

	checkpoints, err := state.checkpointManager.ListCheckpoints()
	if err != nil {
		return nil, fmt.Errorf("unable to list checkpoints: %v", err)
	}

	for _, c := range checkpoints {
		if c == DriverPluginCheckpointFile {
			if err := state.restoreNICs(); err != nil {
				return nil, err
			}
			return state, nil
		}
	}

	checkpoint := newCheckpoint()
	if err := state.checkpointManager.CreateCheckpoint(DriverPluginCheckpointFile, checkpoint); err != nil {
		return nil, fmt.Errorf("unable to sync to checkpoint: %v", err)
	}

	return state, nil
}

func (s *DeviceState) Prepare(ctx context.Context, claim *resourceapi.ResourceClaim) ([]*drapbv1.Device, error) {
	s.Lock()
	defer s.Unlock()

	claimUID := string(claim.UID)

	checkpoint := newCheckpoint()
	if err := s.checkpointManager.GetCheckpoint(DriverPluginCheckpointFile, checkpoint); err != nil {
		return nil, fmt.Errorf("unable to sync from checkpoint: %v", err)
	}
	preparedClaims := checkpoint.V1.PreparedClaims

	if preparedClaims[claimUID] != nil {
		return preparedClaims[claimUID].GetDevices(), nil
	}
	preparedDevices, err := s.prepareDevices(ctx, claim)
	if err != nil {
		return nil, fmt.Errorf("prepare failed: %v", err)
	}

	if err = s.cdi.CreateClaimSpecFile(claimUID, preparedDevices); err != nil {
		return nil, fmt.Errorf("unable to create CDI spec file for claim: %v", err)
	}

	preparedClaims[claimUID] = preparedDevices
	if err := s.checkpointManager.CreateCheckpoint(DriverPluginCheckpointFile, checkpoint); err != nil {
		return nil, fmt.Errorf("unable to sync to checkpoint: %v", err)
	}

	return preparedClaims[claimUID].GetDevices(), nil
}

func (s *DeviceState) Unprepare(claimUID string) error {
	s.Lock()
	defer s.Unlock()

	checkpoint := newCheckpoint()
	if err := s.checkpointManager.GetCheckpoint(DriverPluginCheckpointFile, checkpoint); err != nil {
		checkpoint = newCheckpoint()
		if err := s.checkpointManager.CreateCheckpoint(DriverPluginCheckpointFile, checkpoint); err != nil {
			return fmt.Errorf("unable to create new checkpoint: %v", err)
		}
	}
	preparedClaims := checkpoint.V1.PreparedClaims

	if preparedClaims[claimUID] == nil {
		return nil
	}

	if err := s.unprepareDevices(claimUID, preparedClaims[claimUID]); err != nil {
		return fmt.Errorf("unprepare failed: %v", err)
	}

	err := s.cdi.DeleteClaimSpecFile(claimUID)
	if err != nil {
		return fmt.Errorf("unable to delete CDI spec file for claim: %v", err)
	}

	delete(preparedClaims, claimUID)
	if err := s.checkpointManager.CreateCheckpoint(DriverPluginCheckpointFile, checkpoint); err != nil {
		return fmt.Errorf("unable to sync to checkpoint: %v", err)
	}

	return nil
}

func (s *DeviceState) prepareDevices(ctx context.Context, claim *resourceapi.ResourceClaim) (profiles.PreparedDevices, error) {
	if claim.Status.Allocation == nil {
		return nil, fmt.Errorf("claim not yet allocated")
	}
	// Check if any device request has admin access
	hasAdminAccess := s.checkAdminAccess(claim)

	// Retrieve the full set of device configs for the driver.
	configs, err := GetOpaqueDeviceConfigs(
		s.configDecoder,
		s.driverName,
		claim.Status.Allocation.Devices.Config,
	)
	if err != nil {
		return nil, fmt.Errorf("error getting opaque device configs: %v", err)
	}

	// Add the default device Config to the front of the config list with the
	// lowest precedence. This guarantees there will be at least one config in
	// the list with len(Requests) == 0 for the lookup below.
	configs = slices.Insert(configs, 0, &OpaqueDeviceConfig{})

	// Look through the configs and figure out which one will be applied to
	// each device allocation result based on their order of precedence.
	configResultsMap := make(map[runtime.Object][]*resourceapi.DeviceRequestAllocationResult)
	for _, result := range claim.Status.Allocation.Devices.Results {
		// The claim may include allocations meant for other drivers.
		if result.Driver != s.driverName {
			continue
		}
		if _, exists := s.allocatable[result.Device]; !exists {
			return nil, fmt.Errorf("requested device is not allocatable: %v", result.Device)
		}
		for _, c := range slices.Backward(configs) {
			if len(c.Requests) == 0 || slices.Contains(c.Requests, result.Request) {
				configResultsMap[c.Config] = append(configResultsMap[c.Config], &result)
				break
			}
		}
	}

	// Apply all configs associated with devices that need to be prepared.
	// Track container edits generated from applying the config to the set
	// of device allocation results.
	perDeviceCDIContainerEdits := make(profiles.PerDeviceCDIContainerEdits)
	for config, results := range configResultsMap {
		// Apply the config to the list of results associated with it.
		containerEdits, err := s.configHandler.ApplyConfig(config, results)
		if err != nil {
			return nil, fmt.Errorf("error applying config: %w", err)
		}

		// Merge any new container edits with the overall per result map.
		for k, v := range containerEdits {
			perDeviceCDIContainerEdits[k] = v
		}
	}

	// Flatten configResultsMap into an ordered list of (result, ifaceName) items.
	// Map iteration order isn't stable, so fix it into a slice before doing the
	// concurrent IPAM below and the (order-sensitive) device build after.
	type prepItem struct {
		result    *resourceapi.DeviceRequestAllocationResult
		ifaceName string
	}
	var items []prepItem
	for cfg, results := range configResultsMap {
		// Determine interface name from NicConfig for this batch.
		ifaceName := "net1"
		if s.isNIC {
			if nicCfg, ok := cfg.(*nicconfig.NicConfig); ok && nicCfg != nil && nicCfg.InterfaceName != "" {
				ifaceName = nicCfg.InterfaceName
			}
		}
		for _, result := range results {
			items = append(items, prepItem{result: result, ifaceName: ifaceName})
		}
	}
	if s.isNIC {
		ifaceNames := make([]string, len(items))
		for i, it := range items {
			ifaceNames[i] = it.ifaceName
		}
		if err := s.checkInterfaceNames(claim, ifaceNames); err != nil {
			return nil, err
		}
	}

	// Waiting for kube-ovn-controller's allocation is the slow step. Wait for all
	// claimed NICs concurrently so prepare latency is ~one round-trip instead of N.
	nicCfgs := make([]*nicprepare.NicDeviceConfig, len(items))
	if s.isNIC {
		itemErrs := make([]error, len(items))
		var wg sync.WaitGroup
		sem := make(chan struct{}, maxPrepareConcurrency)
		for i := range items {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				it := items[i]
				dev := s.allocatable[it.result.Device]
				cfg, err := nicprepare.RequestIPAM(ctx, s.coreclient, claim, it.result, dev, it.ifaceName)
				if err != nil {
					itemErrs[i] = fmt.Errorf("NIC IPAM for device %s: %w", it.result.Device, err)
					return
				}
				nicCfgs[i] = cfg
			}(i)
		}
		wg.Wait()
		if err := errors.Join(itemErrs...); err != nil {
			return nil, err
		}
	}

	// Build the prepared devices in item order and hand the attach specs from
	// the (now complete) IPAM results to the NRI hook.
	preparedDevices := make(profiles.PreparedDevices, 0, len(items))
	for i, it := range items {
		result := it.result
		nicCfg := nicCfgs[i]
		// Hand the attach Spec to the NRI sandbox hooks, which fill in
		// NetnsPath and ContainerID once the pod netns exists.
		var nic *plumbing.Spec
		if nicCfg != nil {
			spec := nicSpecFromConfig(nicCfg)
			spec.ClaimUID = string(claim.UID)
			nic = &spec
			if s.nicStore != nil {
				s.nicStore.Add(spec)
			}
		}

		device := &profiles.PreparedDevice{
			Device: drapbv1.Device{
				RequestNames: []string{result.Request},
				PoolName:     result.Pool,
				DeviceName:   result.Device,
				CdiDeviceIds: s.cdi.GetClaimDevices(string(claim.UID), result.Request, result.Device),
			},
			ContainerEdits: perDeviceCDIContainerEdits[profiles.ResultKey(result)],
			AdminAccess:    hasAdminAccess,
			NIC:            nic,
		}
		preparedDevices = append(preparedDevices, device)
	}

	return preparedDevices, nil
}

// unprepareDevices forgets the claim's NICs. Nothing is released:
// kube-ovn-controller owns the NICs' addresses and ports, and the NRI
// StopPodSandbox hook detaches the interfaces.
func (s *DeviceState) unprepareDevices(claimUID string, _ profiles.PreparedDevices) error {
	if s.nicStore != nil {
		s.nicStore.RemoveClaim(claimUID)
	}
	return nil
}

// allocatableDevices returns the devices of the node's pool by name.
func allocatableDevices(resources resourceslice.DriverResources, nodeName string) AllocatableDevices {
	allocatable := make(AllocatableDevices)
	for _, slice := range resources.Pools[nodeName].Slices {
		for _, device := range slice.Devices {
			allocatable[device.Name] = device
		}
	}
	return allocatable
}

// RefreshDevices enumerates the devices again, e.g. after a subnet was added,
// and reports whether they changed. Claims prepared before keep working when
// their device disappears; new claims for it fail as not allocatable.
func (s *DeviceState) RefreshDevices() (resourceslice.DriverResources, bool, error) {
	resources, err := s.profile.EnumerateDevices()
	if err != nil {
		return resourceslice.DriverResources{}, false, fmt.Errorf("enumerate devices: %w", err)
	}
	s.Lock()
	defer s.Unlock()
	if equality.Semantic.DeepEqual(resources, s.driverResources) {
		return resources, false, nil
	}
	s.driverResources = resources
	s.allocatable = allocatableDevices(resources, s.nodeName)
	return resources, true, nil
}

// checkInterfaceNames rejects interface names that two NICs of a pod would
// share: within the claim, or with a NIC of another claim of the pod. Such a
// pod would otherwise only fail when the sandbox starts, on the rename of the
// second NIC in the pod netns. Prepare calls are serialized by the state lock.
func (s *DeviceState) checkInterfaceNames(claim *resourceapi.ResourceClaim, ifaceNames []string) error {
	seen := make(map[string]bool, len(ifaceNames))
	for _, name := range ifaceNames {
		if seen[name] {
			return fmt.Errorf("claim %s/%s requests more than one NIC named %q; set a distinct interfaceName in each request's NicConfig",
				claim.Namespace, claim.Name, name)
		}
		seen[name] = true
	}
	if s.nicStore == nil {
		return nil
	}
	for _, ref := range claim.Status.ReservedFor {
		if ref.Resource != "pods" || ref.APIGroup != "" {
			continue
		}
		for _, spec := range s.nicStore.Specs(string(ref.UID)) {
			if spec.ClaimUID != string(claim.UID) && seen[spec.IfaceName] {
				return fmt.Errorf("NIC %q of claim %s/%s is already used by another DRA NIC of pod %s/%s (claim UID %s); set a distinct interfaceName in the NicConfig",
					spec.IfaceName, claim.Namespace, claim.Name, claim.Namespace, ref.Name, spec.ClaimUID)
			}
		}
	}
	return nil
}

// restoreNICs adds the NICs of the checkpointed claims back to the NIC store
// after a plugin restart, so the NRI hooks can still attach them.
func (s *DeviceState) restoreNICs() error {
	if s.nicStore == nil {
		return nil
	}
	checkpoint := newCheckpoint()
	if err := s.checkpointManager.GetCheckpoint(DriverPluginCheckpointFile, checkpoint); err != nil {
		return fmt.Errorf("unable to sync from checkpoint: %v", err)
	}
	for _, devices := range checkpoint.V1.PreparedClaims {
		for _, device := range devices {
			if device.NIC != nil {
				s.nicStore.Add(*device.NIC)
			}
		}
	}
	return nil
}

// nicSpecFromConfig builds the plumbing.Spec for one NIC from the IPAM result.
// kube-ovn-controller creates a logical switch port for overlay and underlay
// subnets alike (underlay ones reach the VLAN through a localnet port), so
// every NIC is plugged into br-int and bound to its port, like a regular
// kube-ovn pod. NetnsPath and ContainerID are left empty; the NRI sandbox hook
// fills them in when the sandbox appears.
func nicSpecFromConfig(cfg *nicprepare.NicDeviceConfig) plumbing.Spec {
	return plumbing.Spec{
		PodUID:       cfg.PodUID,
		PodName:      cfg.PodName,
		PodNamespace: cfg.PodNamespace,
		IfaceName:    cfg.IfaceName,
		IP:           cfg.IP,
		ExtraIPs:     cfg.ExtraIPs,
		MAC:          cfg.MAC,
		Gateway:      cfg.Gateway,
		Provider:     cfg.Provider,
		IfaceID:      cfg.LogicalSwitchPort,
	}
}

// checkAdminAccess determines if a resource claim requires admin access.
func (s *DeviceState) checkAdminAccess(claim *resourceapi.ResourceClaim) bool {
	if claim != nil && claim.Status.Allocation != nil {
		for _, result := range claim.Status.Allocation.Devices.Results {
			if result.AdminAccess != nil && *result.AdminAccess {
				return true
			}
		}
	}
	return false
}

// GetOpaqueDeviceConfigs returns an ordered list of the configs contained in possibleConfigs for this driver.
//
// Configs can either come from the resource claim itself or from the device
// class associated with the request. Configs coming directly from the resource
// claim take precedence over configs coming from the device class. Moreover,
// configs found later in the list of configs attached to its source take
// precedence over configs found earlier in the list for that source.
//
// All of the configs relevant to the driver from the list of possibleConfigs
// will be returned in order of precedence (from lowest to highest). If no
// configs are found, nil is returned.
func GetOpaqueDeviceConfigs(
	decoder runtime.Decoder,
	driverName string,
	possibleConfigs []resourceapi.DeviceAllocationConfiguration,
) ([]*OpaqueDeviceConfig, error) {
	// Collect all configs in order of reverse precedence.
	var classConfigs []resourceapi.DeviceAllocationConfiguration
	var claimConfigs []resourceapi.DeviceAllocationConfiguration
	var candidateConfigs []resourceapi.DeviceAllocationConfiguration
	for _, config := range possibleConfigs {
		switch config.Source {
		case resourceapi.AllocationConfigSourceClass:
			classConfigs = append(classConfigs, config)
		case resourceapi.AllocationConfigSourceClaim:
			claimConfigs = append(claimConfigs, config)
		default:
			return nil, fmt.Errorf("invalid config source: %v", config.Source)
		}
	}
	candidateConfigs = append(candidateConfigs, classConfigs...)
	candidateConfigs = append(candidateConfigs, claimConfigs...)

	// Decode all configs that are relevant for the driver.
	var resultConfigs []*OpaqueDeviceConfig
	for _, config := range candidateConfigs {
		// If this is nil, the driver doesn't support some future API extension
		// and needs to be updated.
		if config.Opaque == nil {
			return nil, fmt.Errorf("only opaque parameters are supported by this driver")
		}

		// Configs for different drivers may have been specified because a
		// single request can be satisfied by different drivers. This is not
		// an error -- drivers must skip over other driver's configs in order
		// to support this.
		if config.Opaque.Driver != driverName {
			continue
		}

		decodedConfig, err := runtime.Decode(decoder, config.Opaque.Parameters.Raw)
		if err != nil {
			return nil, fmt.Errorf("error decoding config parameters: %w", err)
		}

		resultConfig := &OpaqueDeviceConfig{
			Requests: config.Requests,
			Config:   decodedConfig,
		}

		resultConfigs = append(resultConfigs, resultConfig)
	}

	return resultConfigs, nil
}
