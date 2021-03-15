package hcsshim

import (
	"context"
	"encoding/json"
	"path"
	"time"

	"github.com/Microsoft/hcsshim/hcn"
	"github.com/Microsoft/hcsshim/internal/hcs"
	"github.com/Microsoft/hcsshim/internal/requesttype"
	"github.com/Microsoft/hcsshim/internal/schema1"
	hcsschema "github.com/Microsoft/hcsshim/internal/schema2"
)

type VirtualMachineSpec struct {
	Name      string
	ID        string
	runtimeId string
	spec      *hcsschema.ComputeSystem
	system    *hcs.System
}

func CreateVirtualMachineSpec(name, id, vhdPath, isoPath, owner string, memoryInMB, processorCount int, vnicId, macAddress string) (*VirtualMachineSpec, error) {
	spec := &hcsschema.ComputeSystem{
		Owner: owner,
		SchemaVersion: &hcsschema.Version{
			Major: 2,
			Minor: 1,
		},
		ShouldTerminateOnLastHandleClosed: true,
		VirtualMachine: &hcsschema.VirtualMachine{
			Chipset: &hcsschema.Chipset{
				Uefi: &hcsschema.Uefi{
					BootThis: &hcsschema.UefiBootEntry{
						DevicePath: "primary",
						DeviceType: "ScsiDrive",
						//OptionalData: "ds=nocloud;h=lmasterm;i=test;s=/opt/cloud/metadata",
					},
				},
			},
			ComputeTopology: &hcsschema.Topology{
				Memory: &hcsschema.Memory2{
					SizeInMB: int32(memoryInMB),
				},
				Processor: &hcsschema.Processor2{
					Count: int32(processorCount),
				},
			},
			Devices: &hcsschema.Devices{
				Scsi: map[string]hcsschema.Scsi{
					"primary": hcsschema.Scsi{
						Attachments: map[string]hcsschema.Attachment{
							"0": hcsschema.Attachment{
								Path:  vhdPath,
								Type_: "VirtualDisk",
							},
							"1": hcsschema.Attachment{
								Path:  isoPath,
								Type_: "Iso",
							},
						},
					},
				},
				NetworkAdapters: map[string]hcsschema.NetworkAdapter{},
			},
			// GuestConnection: &hcsschema.GuestConnection{
			//	UseVsock:            true,
			//	UseConnectedSuspend: true,
			//},
		},
	}

	if len(vnicId) > 0 {
		spec.VirtualMachine.Devices.NetworkAdapters["ext"] = hcsschema.NetworkAdapter{
			EndpointId: vnicId,
			MacAddress: macAddress,
		}
	}

	return &VirtualMachineSpec{
		spec: spec,
		ID:   id,
		Name: name,
	}, nil
}

func getHcsSpec(system *hcs.System) *hcsschema.ComputeSystem {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()

	_, err := system.Properties(ctx)
	if err != nil {
		return nil
	}
	// FixMe - return proper Compute System schema
	return nil
}

func GetVirtualMachineState(id string) string {
	properties, err := GetVirtualMachineProperties(id)
	if err != nil {
		return ""
	}
	return properties.State
}

func GetVirtualMachineProperties(id string) (*schema1.ContainerProperties, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()
	system, err := hcs.OpenComputeSystem(ctx, id)
	if err != nil {
		return nil, err
	}
	defer system.Close()

	return system.Properties(ctx)
}

func GetVirtualMachineSpec(id string) (*VirtualMachineSpec, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()
	system, err := hcs.OpenComputeSystem(ctx, id)
	if err != nil {
		return nil, err
	}

	return &VirtualMachineSpec{
		ID:     id,
		system: system,
		spec:   getHcsSpec(system),
	}, nil

}

// HasVirtualMachine
func HasVirtualMachine(id string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()
	system, err := hcs.OpenComputeSystem(ctx, id)
	if err != nil {
		if hcs.IsNotExist(err) {
			return false
		} else {
			return true
		}
	}
	defer system.Close()

	return true
}

// List all/specified Virtual Machine
func ListVirtualMachines(id string) ([]*VirtualMachineSpec, error) {

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()
	query := ComputeSystemQuery{
		Types: []string{"VirtualMachine"},
	}
	if len(id) != 0 {
		query.IDs = []string{id}
	}

	vms := []*VirtualMachineSpec{}
	vmproperties, err := hcs.GetComputeSystems(ctx, query)
	if err != nil {
		return vms, err
	}

	for _, vmprop := range vmproperties {
		vm, err := GetVirtualMachineSpec(vmprop.ID)
		if err != nil {
			return vms, err
		}
		vms = append(vms, vm)

	}

	return vms, nil

}

// Create a Virtual Machine
func (vm *VirtualMachineSpec) Create() error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()
	system, err := hcs.CreateComputeSystem(ctx, vm.ID, vm.spec)
	if err != nil {
		return err
	}
	properties, err := system.Properties(ctx)
	if err != nil {
		return err
	}

	vm.runtimeId = properties.RuntimeID.String()
	vm.system = system

	return nil
}

// Start Virtual Machine
func (vm *VirtualMachineSpec) Start() error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()
	system, err := hcs.OpenComputeSystem(ctx, vm.ID)
	if err != nil {
		return err
	}
	defer system.Close()

	return system.Start(ctx)
}

// Stop a Virtual Machine
func (vm *VirtualMachineSpec) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()
	system, err := hcs.OpenComputeSystem(ctx, vm.ID)
	if err != nil {
		return err
	}
	defer system.Close()

	return system.Shutdown(ctx)
}

// Delete a Virtual Machine
func (vm *VirtualMachineSpec) Delete() error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()
	system, err := hcs.OpenComputeSystem(ctx, vm.ID)
	if err != nil {
		if hcs.IsNotExist(err) {
			return nil
		} else {
			return err
		}
	}
	defer system.Close()

	return system.Terminate(ctx)
}

// Wait for a Virtual Machine exits
func (vm *VirtualMachineSpec) Wait() error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()
	system, err := hcs.OpenComputeSystem(ctx, vm.ID)
	if err != nil {
		return err
	}
	defer system.Close()

	return system.Wait()
}

// ExecuteCommand executes a command in the Virtual Machine
func (vm *VirtualMachineSpec) ExecuteCommand(command string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()
	system, err := hcs.OpenComputeSystem(ctx, vm.ID)
	if err != nil {
		return err
	}
	defer system.Close()

	return nil
}

func (vm *VirtualMachineSpec) HotAttachEndpoints(endpoints []*hcn.HostComputeEndpoint) (err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()
	system, err := hcs.OpenComputeSystem(ctx, vm.ID)
	if err != nil {
		return err
	}
	defer system.Close()

	for _, endpoint := range endpoints {
		if err = vm.hotAttachEndpoint(ctx, system, endpoint); err != nil {
			return err
		}
	}
	return nil
}

func (vm *VirtualMachineSpec) HotDetachEndpoint(endpoint *hcn.HostComputeEndpoint) (err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()
	system, err := hcs.OpenComputeSystem(ctx, vm.ID)
	if err != nil {
		return err
	}
	defer system.Close()

	// Hot detach an endpoint from the compute system
	request := hcsschema.ModifySettingRequest{
		RequestType:  requesttype.Remove,
		ResourcePath: path.Join("VirtualMachine/Devices/NetworkAdapters", endpoint.Id),
		Settings: hcsschema.NetworkAdapter{
			EndpointId: endpoint.Id,
			MacAddress: endpoint.MacAddress,
		},
	}

	if err = system.Modify(ctx, request); err != nil {
		return err
	}

	return nil
}

func (vm *VirtualMachineSpec) hotAttachEndpoint(ctx context.Context, system *hcs.System, endpoint *hcn.HostComputeEndpoint) (err error) {
	// Hot attach an endpoint to the compute system
	request := hcsschema.ModifySettingRequest{
		RequestType:  requesttype.Add,
		ResourcePath: path.Join("VirtualMachine/Devices/NetworkAdapters", endpoint.Id),
		Settings: hcsschema.NetworkAdapter{
			EndpointId: endpoint.Id,
			MacAddress: endpoint.MacAddress,
		},
	}

	if err = system.Modify(ctx, request); err != nil {
		return err
	}

	return nil
}

func (vm *VirtualMachineSpec) String() string {
	jsonString, err := json.Marshal(vm.spec)
	if err != nil {
		return ""
	}

	return string(jsonString)
}
