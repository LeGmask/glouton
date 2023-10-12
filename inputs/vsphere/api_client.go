// Copyright 2015-2023 Bleemeo
//
// bleemeo.com an infrastructure monitoring solution in the Cloud
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package vsphere

import (
	"context"
	"errors"
	"fmt"
	"glouton/config"
	"glouton/facts"
	"net/url"
	"strings"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/soap"
)

//nolint:gochecknoglobals
var (
	relevantClusterProperties []string // An empty list will retrieve all properties.
	relevantHostProperties    = []string{
		"hardware.cpuInfo.numCpuCores",
		"summary.hardware.cpuModel",
		"summary.config.name",
		"config.option",
		"hardware.memorySize",
		"config.product.osType",
		"summary.hardware.model",
		"summary.hardware.vendor",
		"config.dateTimeInfo.timeZone.name",
		"config.network.dnsConfig.domainName",
		"config.network.vnic",
		// config.vmotion.ipConfig.ipAddress ?
		// config.ipmi.bmcIpAddress ?
		// config.ipmi.bmcMacAddress ?
		"config.network.ipV6Enabled",
		"config.product.version",
		"summary.config.vmotionEnabled",

		"runtime.powerState", // Only used for generating a status
	}
	relevantVMProperties = []string{
		"config.hardware.numCPU",
		"guest.hostName",
		"config.hardware.memoryMB",
		"config.guestFullName",
		"summary.config.product.name",   // Don't really know if the Product
		"summary.config.product.vendor", // section is sometime not null...
		"guest.ipAddress",
		"runtime.host",
		"resourcePool",
		"config.datastoreUrl",
		"config.version",
		"config.name",

		"runtime.powerState", // Only used for generating a status
	}
)

func newDeviceFinder(ctx context.Context, vSphereCfg config.VSphere) (*find.Finder, error) {
	u, err := soap.ParseURL(vSphereCfg.URL)
	if err != nil {
		return nil, err
	}

	u.User = url.UserPassword(vSphereCfg.Username, vSphereCfg.Password)

	c, err := govmomi.NewClient(ctx, u, vSphereCfg.InsecureSkipVerify)
	if err != nil {
		return nil, err
	}

	f := find.NewFinder(c.Client, true)

	dc, err := f.DefaultDatacenter(ctx)
	if err != nil {
		return nil, err
	}

	// Make future calls to the local datacenter
	f.SetDatacenter(dc)

	return f, nil
}

func findDevices(ctx context.Context, finder *find.Finder) (clusters []*object.ClusterComputeResource, hosts []*object.HostSystem, vms []*object.VirtualMachine, err error) {
	// The find.NotFoundError is thrown when no devices are found,
	// even if the path is not restrictive to a particular device.
	var notFoundError *find.NotFoundError

	clusters, err = finder.ClusterComputeResourceList(ctx, "*")
	if err != nil && !errors.As(err, &notFoundError) {
		return nil, nil, nil, err
	}

	hosts, err = finder.HostSystemList(ctx, "*")
	if err != nil && !errors.As(err, &notFoundError) {
		return nil, nil, nil, err
	}

	vms, err = finder.VirtualMachineList(ctx, "*")
	if err != nil && !errors.As(err, &notFoundError) {
		return nil, nil, nil, err
	}

	return clusters, hosts, vms, nil
}

func describeCluster(cluster *object.ClusterComputeResource, clusterProps mo.ClusterComputeResource) *Cluster {
	clusterFacts := make(map[string]string)

	if resourceSummary := clusterProps.Summary.GetComputeResourceSummary(); resourceSummary != nil {
		clusterFacts["hosts"] = str(resourceSummary.NumHosts)
		clusterFacts["cpu_cores"] = str(resourceSummary.NumCpuCores)
	}

	datastores := make([]string, len(clusterProps.Datastore))

	for i, ds := range clusterProps.Datastore {
		datastores[i] = ds.Value
	}

	dev := device{
		source: cluster.Client().URL().Host,
		moid:   cluster.Reference().Value,
		name:   cluster.Name(),
		facts:  clusterFacts,
		// TODO: Rename powerState to status and store the cluster's overallStatus ? (green)
	}

	dev.facts["fqdn"] = dev.FQDN()

	return &Cluster{
		device:     dev,
		datastores: datastores,
	}
}

func describeHost(host *object.HostSystem, hostProps mo.HostSystem) *HostSystem {
	hostFacts := map[string]string{
		"cpu_cores":      str(hostProps.Summary.Hardware.NumCpuCores),
		"cpu_model_name": hostProps.Summary.Hardware.CpuModel,
		"hostname":       hostProps.Summary.Config.Name,
		"product_name":   hostProps.Summary.Hardware.Model,
		"system_vendor":  hostProps.Summary.Hardware.Vendor,
		// custom
		"vsphere_vmotion_enabled": str(hostProps.Summary.Config.VmotionEnabled),
	}

	if hostProps.Hardware != nil {
		hostFacts["cpu_cores"] = str(hostProps.Hardware.CpuInfo.NumCpuCores)
		hostFacts["memory"] = facts.ByteCountDecimal(uint64(hostProps.Hardware.MemorySize))
	}

	if hostProps.Config != nil {
		hostFacts["os_pretty_name"] = hostProps.Config.Product.OsType
		hostFacts["ipv6_enabled"] = str(*hostProps.Config.Network.IpV6Enabled)
		hostFacts["vsphere_host_version"] = hostProps.Config.Product.Version

		if hostProps.Config.DateTimeInfo != nil {
			hostFacts["timezone"] = hostProps.Config.DateTimeInfo.TimeZone.Name
		}

		if hostProps.Config.Network != nil {
			if dns := hostProps.Config.Network.DnsConfig; dns != nil {
				if cfg := dns.GetHostDnsConfig(); cfg != nil {
					hostFacts["domain"] = cfg.DomainName
				}
			}

			if vnic := hostProps.Config.Network.Vnic; len(vnic) > 0 {
				if vnic[0].Spec.Ip != nil {
					hostFacts["primary_address"] = vnic[0].Spec.Ip.IpAddress
				}
			}
		}

		if hostFacts["hostname"] == "" {
			for _, opt := range hostProps.Config.Option {
				if optValue := opt.GetOptionValue(); optValue != nil {
					if optValue.Key == "Misc.HostName" {
						hostFacts["hostname"], _ = optValue.Value.(string)

						break
					}
				}
			}
		}
	}

	dev := device{
		source:     host.Client().URL().Host,
		moid:       host.Reference().Value,
		name:       fallback(hostFacts["hostname"], host.Reference().Value),
		facts:      hostFacts,
		powerState: string(hostProps.Runtime.PowerState),
	}

	dev.facts["fqdn"] = dev.FQDN()

	return &HostSystem{dev}
}

func describeVM(ctx context.Context, vm *object.VirtualMachine, vmProps mo.VirtualMachine) *VirtualMachine {
	vmFacts := make(map[string]string)

	if vmProps.Config != nil {
		vmFacts["cpu_cores"] = str(vmProps.Config.Hardware.NumCPU)
		vmFacts["memory"] = facts.ByteCountDecimal(uint64(vmProps.Config.Hardware.MemoryMB) * 1 << 20) // MB to B
		vmFacts["vsphere_vm_version"] = vmProps.Config.Version
		vmFacts["vsphere_vm_name"] = vmProps.Config.Name

		if vmProps.Config.GuestFullName != "otherGuest" {
			vmFacts["os_pretty_name"] = vmProps.Config.GuestFullName
		}

		if vmProps.Summary.Config.Product != nil {
			vmFacts["product_name"] = vmProps.Summary.Config.Product.Name
			vmFacts["system_vendor"] = vmProps.Summary.Config.Product.Vendor
		}

		if datastores := vmProps.Config.DatastoreUrl; len(datastores) > 0 {
			dsNames := make([]string, len(datastores))
			for i, datastore := range vmProps.Config.DatastoreUrl {
				dsNames[i] = datastore.Name
			}

			vmFacts["vsphere_datastore"] = strings.Join(dsNames, ", ")
		}
	}

	if vmProps.Runtime.Host != nil {
		vmFacts["vsphere_host"] = vmProps.Runtime.Host.Value
	}

	if vmProps.ResourcePool != nil {
		vmFacts["vsphere_resource_pool"] = vmProps.ResourcePool.Value
	}

	if vmProps.Guest != nil {
		vmFacts["primary_address"] = vmProps.Guest.IpAddress
	}

	switch {
	case vmProps.Guest != nil && vmProps.Guest.HostName != "":
		vmFacts["hostname"] = vmProps.Guest.HostName
	case vmProps.Summary.Vm != nil:
		vmFacts["hostname"] = vmProps.Summary.Vm.Value
	default:
		vmFacts["hostname"] = vm.Reference().Value
	}

	dev := device{
		source:     vm.Client().URL().Host,
		moid:       vm.Reference().Value,
		name:       fallback(vmFacts["vsphere_vm_name"], vm.Reference().Value),
		facts:      vmFacts,
		powerState: string(vmProps.Runtime.PowerState),
	}

	dev.facts["fqdn"] = dev.FQDN()

	return &VirtualMachine{
		device: dev,
		UUID:   vm.UUID(ctx),
	}
}

func str(v any) string { return fmt.Sprint(v) }

func fallback(v string, otherwise string) string {
	if v != "" {
		return v
	}

	return otherwise
}
