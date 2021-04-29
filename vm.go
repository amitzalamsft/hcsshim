package hcsshim

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"time"

	"github.com/Microsoft/hcsshim/hcn"
	"github.com/Microsoft/hcsshim/internal/guestrequest"
	"github.com/Microsoft/hcsshim/internal/hcs"
	"github.com/Microsoft/hcsshim/internal/requesttype"
	"github.com/Microsoft/hcsshim/internal/schema1"
	hcsschema "github.com/Microsoft/hcsshim/internal/schema2"
	"github.com/Microsoft/hcsshim/internal/wclayer"
	"github.com/Microsoft/hcsshim/osversion"
)

type GpuAssignmentMode string

const (
	GpuAssignmentModeDisabled = GpuAssignmentMode("Disabled")
	GpuAssignmentModeDefault  = GpuAssignmentMode("Default")
	GpuAssignmentModeList     = GpuAssignmentMode("List")
	GpuAssignmentModeMirror   = GpuAssignmentMode("Mirror")
)

type VirtualMachineOptions struct {
	Name               string
	Id                 string
	VhdPath            string
	IsoPath            string
	Owner              string
	MemoryInMB         int32
	ProcessorCount     int32
	VnicId             string
	MacAddress         string
	UseGuestConnection bool
}

const plan9Port = 564

type VirtualMachineSpec struct {
	Name      string
	ID        string
	runtimeId string
	spec      *hcsschema.ComputeSystem
	system    *hcs.System
}

func CreateVirtualMachineSpec(opts *VirtualMachineOptions) (*VirtualMachineSpec, error) {
	// Ensure the VM has access, we use opts.Id to create VM
	if err := wclayer.GrantVmAccess(opts.Id, opts.VhdPath); err != nil {
		return nil, err
	}
	if err := wclayer.GrantVmAccess(opts.Id, opts.IsoPath); err != nil {
		return nil, err
	}

	spec := &hcsschema.ComputeSystem{
		Owner: opts.Owner,
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
					SizeInMB: int32(opts.MemoryInMB),
				},
				Processor: &hcsschema.Processor2{
					Count: int32(opts.ProcessorCount),
				},
			},
			Devices: &hcsschema.Devices{
				Scsi: map[string]hcsschema.Scsi{
					"primary": hcsschema.Scsi{
						Attachments: map[string]hcsschema.Attachment{
							"0": hcsschema.Attachment{
								Path:  opts.VhdPath,
								Type_: "VirtualDisk",
							},
							"1": hcsschema.Attachment{
								Path:  opts.IsoPath,
								Type_: "Iso",
							},
						},
					},
				},
				NetworkAdapters: map[string]hcsschema.NetworkAdapter{},
				Plan9:           &hcsschema.Plan9{},
			},
		},
	}

	if len(opts.VnicId) > 0 {
		spec.VirtualMachine.Devices.NetworkAdapters["ext"] = hcsschema.NetworkAdapter{
			EndpointId: opts.VnicId,
			MacAddress: opts.MacAddress,
		}
	}

	if opts.UseGuestConnection {
		spec.VirtualMachine.GuestConnection = &hcsschema.GuestConnection{
			UseVsock:            true,
			UseConnectedSuspend: true,
		}
	}

	return &VirtualMachineSpec{
		spec: spec,
		ID:   opts.Id,
		Name: opts.Name,
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

// AddPlan9 adds a Plan9 share to a VirtualMachineSpec.
func (vm *VirtualMachineSpec) AddPlan9(shareName string, hostPath string, uvmPath string, readOnly bool, restrict bool, allowedNames []string) (err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()

	system, err := hcs.OpenComputeSystem(ctx, vm.ID)
	if err != nil {
		return err
	}
	defer system.Close()

	if restrict && osversion.Get().Build < 18328 {
		return errors.New("single-file mappings are not supported on this build of Windows")
	}
	if uvmPath == "" {
		return fmt.Errorf("uvmPath must be passed to AddPlan9")
	}

	// TODO: JTERRY75 - These are marked private in the schema. For now use them
	// but when there are public variants we need to switch to them.
	const (
		shareFlagsReadOnly           int32 = 0x00000001
		shareFlagsLinuxMetadata      int32 = 0x00000004
		shareFlagsCaseSensitive      int32 = 0x00000008
		shareFlagsRestrictFileAccess int32 = 0x00000080
	)

	// TODO: JTERRY75 - `shareFlagsCaseSensitive` only works if the Windows
	// `hostPath` supports case sensitivity. We need to detect this case before
	// forwarding this flag in all cases.
	flags := shareFlagsLinuxMetadata // | shareFlagsCaseSensitive
	if readOnly {
		flags |= shareFlagsReadOnly
	}
	if restrict {
		flags |= shareFlagsRestrictFileAccess
	}

	modification := &hcsschema.ModifySettingRequest{
		RequestType: requesttype.Add,
		Settings: hcsschema.Plan9Share{
			Name:         shareName,
			AccessName:   shareName,
			Path:         hostPath,
			Port:         plan9Port,
			Flags:        flags,
			AllowedFiles: allowedNames,
		},
		ResourcePath: fmt.Sprintf("VirtualMachine/Devices/Plan9/Shares"),
		GuestRequest: guestrequest.GuestRequest{
			ResourceType: guestrequest.ResourceTypeMappedDirectory,
			RequestType:  requesttype.Add,
			Settings: guestrequest.LCOWMappedDirectory{
				MountPath: uvmPath,
				ShareName: shareName,
				Port:      plan9Port,
				ReadOnly:  readOnly,
			},
		},
	}

	if err := system.Modify(ctx, modification); err != nil {
		return err
	}

	return nil
}

func (vm *VirtualMachineSpec) RemovePlan9(shareName string, uvmPath string) (err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()

	system, err := hcs.OpenComputeSystem(ctx, vm.ID)
	if err != nil {
		return err
	}
	defer system.Close()

	modification := &hcsschema.ModifySettingRequest{
		RequestType: requesttype.Remove,
		Settings: hcsschema.Plan9Share{
			Name:       shareName,
			AccessName: shareName,
			Port:       plan9Port,
		},
		ResourcePath: fmt.Sprintf("VirtualMachine/Devices/Plan9/Shares"),
		GuestRequest: guestrequest.GuestRequest{
			ResourceType: guestrequest.ResourceTypeMappedDirectory,
			RequestType:  requesttype.Remove,
			Settings: guestrequest.LCOWMappedDirectory{
				MountPath: uvmPath,
				ShareName: shareName,
				Port:      plan9Port,
			},
		},
	}
	if err := system.Modify(ctx, modification); err != nil {
		return fmt.Errorf("failed to remove plan9 share %s from %s: %+v: %s", shareName, vm.ID, modification, err)
	}
	return nil
}

func (vm *VirtualMachineSpec) UpdateGpuConfiguration(mode GpuAssignmentMode, allowVendorExtension bool, assignments map[string]uint16) (err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()

	system, err := hcs.OpenComputeSystem(ctx, vm.ID)
	if err != nil {
		return err
	}
	defer system.Close()

	settings := hcsschema.GpuConfiguration{
		AssignmentMode:       string(mode),
		AllowVendorExtension: allowVendorExtension,
	}

	if len(assignments) != 0 {
		settings.AssignmentRequest = assignments
	}

	request := hcsschema.ModifySettingRequest{
		RequestType:  requesttype.Update,
		ResourcePath: "VirtualMachine/ComputeTopology/Gpu",
		Settings:     settings,
	}

	if err := system.Modify(ctx, request); err != nil {
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
