package main

import (
	"encoding/json"

	"k8s.io/kubernetes/pkg/kubelet/checkpointmanager"
	"k8s.io/kubernetes/pkg/kubelet/checkpointmanager/checksum"

	"sigs.k8s.io/dra-example-driver/internal/profiles"
)

type PreparedClaims map[string]profiles.PreparedDevices

type Checkpoint struct {
	Checksum checksum.Checksum `json:"checksum"`
	V1       *CheckpointV1     `json:"v1,omitempty"`
}

var _ checkpointmanager.Checkpoint = &Checkpoint{}

type CheckpointV1 struct {
	PreparedClaims PreparedClaims `json:"preparedClaims,omitempty"`
}

func newCheckpoint() *Checkpoint {
	pc := &Checkpoint{
		Checksum: 0,
		V1: &CheckpointV1{
			PreparedClaims: make(PreparedClaims),
		},
	}
	return pc
}

func (cp *Checkpoint) GetPreparedDevices(claimUID string) profiles.PreparedDevices {
	if cp.V1 == nil {
		return nil
	}
	if devices, ok := cp.V1.PreparedClaims[claimUID]; ok {
		return devices
	}
	return nil
}

func (cp *Checkpoint) AddPreparedDevices(claimUID string, pds profiles.PreparedDevices) {
	if cp.V1 == nil {
		return
	}

	cp.V1.PreparedClaims[claimUID] = pds
}

func (cp *Checkpoint) RemovePreparedDevices(claimUID string) {
	if cp.V1 == nil {
		return
	}

	delete(cp.V1.PreparedClaims, claimUID)
}

func (cp *Checkpoint) MarshalCheckpoint() ([]byte, error) {
	cp.Checksum = 0
	out, err := json.Marshal(*cp)
	if err != nil {
		return nil, err
	}
	cp.Checksum = checksum.New(out)
	return json.Marshal(*cp)
}

func (cp *Checkpoint) UnmarshalCheckpoint(data []byte) error {
	return json.Unmarshal(data, cp)
}

func (cp *Checkpoint) VerifyChecksum() error {
	ck := cp.Checksum
	cp.Checksum = 0
	defer func() {
		cp.Checksum = ck
	}()
	out, err := json.Marshal(*cp)
	if err != nil {
		return err
	}
	return ck.Verify(out)
}
