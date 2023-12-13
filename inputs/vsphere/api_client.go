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
	"glouton/logger"
	"maps"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25"
	"github.com/vmware/govmomi/vim25/soap"
	"github.com/vmware/govmomi/vim25/types"
)

func newDeviceFinder(ctx context.Context, vSphereCfg config.VSphere) (*find.Finder, *vim25.Client, error) {
	u, err := soap.ParseURL(vSphereCfg.URL)
	if err != nil {
		return nil, nil, err
	}

	u.User = url.UserPassword(vSphereCfg.Username, vSphereCfg.Password)

	c, err := govmomi.NewClient(ctx, u, vSphereCfg.InsecureSkipVerify)
	if err != nil {
		return nil, nil, err
	}

	f := find.NewFinder(c.Client, true)

	dc, err := f.DefaultDatacenter(ctx)
	if err != nil {
		return nil, nil, err
	}

	// Make future calls to the local datacenter
	f.SetDatacenter(dc)

	return f, c.Client, nil
}

func findDevices(ctx context.Context, finder *find.Finder, listDatastores bool) (clusters []*object.ClusterComputeResource, datastores []*object.Datastore, resourcePools []*object.ResourcePool, hosts []*object.HostSystem, vms []*object.VirtualMachine, err error) {
	// The find.NotFoundError is thrown when no devices are found,
	// even if the path is not restrictive to a particular device.
	var notFoundError *find.NotFoundError

	clusters, err = finder.ClusterComputeResourceList(ctx, "*")
	if err != nil && !errors.As(err, &notFoundError) {
		return nil, nil, nil, nil, nil, err
	}

	if listDatastores {
		datastores, err = finder.DatastoreList(ctx, "*")
		if err != nil && !errors.As(err, &notFoundError) {
			return nil, nil, nil, nil, nil, err
		}
	}

	resourcePools, err = finder.ResourcePoolList(ctx, "*")
	if err != nil && !errors.As(err, &notFoundError) {
		return nil, nil, nil, nil, nil, err
	}

	hosts, err = finder.HostSystemList(ctx, "*")
	if err != nil && !errors.As(err, &notFoundError) {
		return nil, nil, nil, nil, nil, err
	}

	vms, err = finder.VirtualMachineList(ctx, "*")
	if err != nil && !errors.As(err, &notFoundError) {
		return nil, nil, nil, nil, nil, err
	}

	return clusters, datastores, resourcePools, hosts, vms, nil
}

func describeCluster(source string, rfName refName, clusterProps clusterLightProps) *Cluster {
	clusterFacts := make(map[string]string)

	if resourceSummary := clusterProps.ComputeResource.Summary; resourceSummary != nil {
		clusterFacts["cpu_cores"] = str(resourceSummary.ComputeResourceSummary.NumCpuCores)
	}

	datastores := make([]string, len(clusterProps.ComputeResource.Datastore))

	for i, ds := range clusterProps.ComputeResource.Datastore {
		datastores[i] = ds.Value
	}

	dev := device{
		source: source,
		moid:   rfName.Reference().Value,
		name:   rfName.Name(),
		facts:  clusterFacts,
		state:  string(clusterProps.ComputeResource.ManagedEntity.OverallStatus),
	}

	dev.facts["fqdn"] = dev.FQDN()

	return &Cluster{
		device:     dev,
		datastores: datastores,
	}
}

func describeHost(source string, rfName refName, hostProps hostLightProps) *HostSystem {
	hostFacts := map[string]string{
		"hostname": hostProps.Summary.Config.Name,
		// custom
		"vsphere_vmotion_enabled": str(hostProps.Summary.Config.VmotionEnabled),
	}

	if hostProps.Summary.Hardware != nil {
		hostFacts["cpu_model_name"] = hostProps.Summary.Hardware.CpuModel
		hostFacts["product_name"] = hostProps.Summary.Hardware.Model
		hostFacts["system_vendor"] = hostProps.Summary.Hardware.Vendor
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
			hostFacts["hostname"] = hostProps.ManagedEntity.Name
		}
	}

	dev := device{
		source: source,
		moid:   rfName.Reference().Value,
		name:   fallback(hostFacts["hostname"], rfName.Name()),
		facts:  hostFacts,
		state:  string(hostProps.Runtime.PowerState),
	}

	dev.facts["fqdn"] = dev.FQDN()

	return &HostSystem{dev}
}

func describeVM(source string, rfName refName, vmProps vmLightProps, h *Hierarchy) (*VirtualMachine, map[string]string, map[string]string) {
	vmFacts := make(map[string]string)

	var (
		disks         map[string]string
		netInterfaces map[string]string
	)

	if vmProps.Config != nil {
		vmFacts["cpu_cores"] = str(vmProps.Config.Hardware.NumCPU)
		vmFacts["memory"] = facts.ByteCountDecimal(uint64(vmProps.Config.Hardware.MemoryMB) * 1 << 20) // MB to B
		vmFacts["vsphere_vm_version"] = vmProps.Config.Version
		vmFacts["vsphere_vm_name"] = vmProps.Config.Name

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

		disks, netInterfaces = getVMLabelsMetadata(vmProps.Config.Hardware.Device)
	}

	if vmProps.Runtime.Host != nil {
		host := vmProps.Runtime.Host.Value
		if hostName, ok := h.DeviceName(host); ok {
			host = hostName
		}

		vmFacts["vsphere_host"] = host
	}

	if vmProps.ResourcePool != nil {
		resourcePool := vmProps.ResourcePool.Value
		if resourcePoolName, ok := h.DeviceName(resourcePool); ok {
			resourcePool = resourcePoolName
		}

		vmFacts["vsphere_resource_pool"] = resourcePool
	}

	if vmProps.Guest != nil {
		vmFacts["os_pretty_name"] = vmProps.Guest.GuestFullName

		if vmProps.Guest.IpAddress != "" {
			vmFacts["primary_address"] = vmProps.Guest.IpAddress
		}
	}

	if vmFacts["os_pretty_name"] == "" && vmProps.Config != nil && vmProps.Config.GuestFullName != "otherGuest" {
		vmFacts["os_pretty_name"] = vmProps.Config.GuestFullName
	}

	switch {
	case vmProps.Guest != nil && vmProps.Guest.HostName != "":
		vmFacts["hostname"] = vmProps.Guest.HostName
	case vmProps.Summary.Vm != nil:
		vmFacts["hostname"] = vmProps.Summary.Vm.Value
	default:
		vmFacts["hostname"] = rfName.Name()
	}

	dev := device{
		source: source,
		moid:   rfName.Reference().Value,
		name:   fallback(vmFacts["vsphere_vm_name"], rfName.Name()),
		facts:  vmFacts,
		state:  string(vmProps.Runtime.PowerState),
	}

	dev.facts["fqdn"] = dev.FQDN()

	return &VirtualMachine{dev}, disks, netInterfaces
}

func getVMLabelsMetadata(devices object.VirtualDeviceList) (map[string]string, map[string]string) {
	disks := make(map[string]string)
	netInterfaces := make(map[string]string)

	for _, device := range devices {
		switch dev := device.(type) {
		case *types.VirtualDisk:
			if dev.UnitNumber == nil {
				continue
			}

			switch controller := devices.FindByKey(dev.ControllerKey).(type) {
			case types.BaseVirtualSCSIController:
				disks[fmt.Sprintf("scsi%d:%d", controller.GetVirtualSCSIController().BusNumber, *dev.UnitNumber)] = devices.Name(dev)
			case types.BaseVirtualSATAController:
				disks[fmt.Sprintf("sata%d:%d", controller.GetVirtualSATAController().BusNumber, *dev.UnitNumber)] = devices.Name(dev)
			case *types.VirtualNVMEController:
				disks[fmt.Sprintf("nvme%d:%d", controller.BusNumber, *dev.UnitNumber)] = devices.Name(dev)
			default:
				logger.V(2).Printf("Unknown disk controller: %T", controller)
			}
		case types.BaseVirtualEthernetCard: // VirtualVmxnet, VirtualE1000, ...
			ethernetCard := dev.GetVirtualEthernetCard()

			netInterfaces[strconv.Itoa(int(ethernetCard.Key))] = devices.Name(ethernetCard)
		}
	}

	return disks, netInterfaces
}

func str(v any) string { return fmt.Sprint(v) }

func fallback(v string, otherwise string) string {
	if v != "" {
		return v
	}

	return otherwise
}

type commonObject interface {
	Reference() types.ManagedObjectReference
	Name() string
}

var isolateLUN = regexp.MustCompile(`.*/([^/]+)/?$`)

func getDatastorePerLUN(ctx context.Context, client *vim25.Client, datastores []*object.Datastore, cache *propsCache[datastoreLightProps]) (map[string]string, error) {
	dsProps, err := retrieveProps(ctx, client, datastores, relevantDatastoreProperties, cache)
	if err != nil {
		logger.V(1).Printf("Failed to retrieve datastore props of %s: %v", client.URL().Host, err)

		return map[string]string{}, err
	}

	dsPerLUN := make(map[string]string, len(dsProps))

	for ds, props := range dsProps {
		info := props.Info.GetDatastoreInfo()
		if info == nil {
			continue
		}

		matches := isolateLUN.FindStringSubmatch(info.Url)
		if matches == nil {
			continue
		}

		dsPerLUN[matches[1]] = ds.Name()
	}

	return dsPerLUN, nil
}

func additionalClusterMetrics(ctx context.Context, client *vim25.Client, clusters []*object.ClusterComputeResource, caches *propsCaches, acc telegraf.Accumulator, retained retainedMetrics, h *Hierarchy, t0 time.Time) error {
	for _, cluster := range clusters {
		// Generating running & stopped hosts count metrics
		hosts, err := cluster.Hosts(ctx)
		if err != nil {
			return err
		}

		hostProps, err := retrieveProps(ctx, client, hosts, relevantHostProperties, caches.hostCache)
		if err != nil {
			return err
		}

		var running, stopped int

		for _, props := range hostProps {
			if props.Runtime.PowerState == types.HostSystemPowerStatePoweredOn {
				running++
			} else {
				stopped++
			}
		}

		fields := map[string]any{
			"running_count": running,
			"stopped_count": stopped,
		}

		tags := map[string]string{
			"clustername": cluster.Name(),
			"dcname":      h.ParentDCName(cluster),
			"moid":        cluster.Reference().Value,
		}

		acc.AddFields("hosts", fields, tags, t0)

		// Generating cpu used metric

		logger.Printf("Retained: %v", retained["vsphere_cluster_cpu"])

		usagesMHz := retained.get("vsphere_cluster_cpu", "usagemhz_average", func(tags map[string]string, t []time.Time) bool {
			// the device is the cluster we want && timestamp matches
			return tags["moid"] == cluster.Reference().Value && (len(t) == 0 || t[0].Equal(t0))
		})

		clusterProps, ok := caches.clusterCache.get(cluster.Reference().Value, true)
		if len(usagesMHz) == 0 || !ok {
			return nil
		}

		var sum int64

		for _, value := range usagesMHz {
			sum += value.(int64) //nolint: forcetypeassert
		}

		avg := float64(sum) / float64(len(usagesMHz))

		totalMHz := float64(clusterProps.ComputeResource.Summary.ComputeResourceSummary.UsageSummary.TotalCpuCapacityMhz)
		if totalMHz == 0 { // using dummy value for vcsim, where the total is always 0
			totalMHz = avg * 2
		}

		result := (avg / totalMHz) * 100

		logger.Printf("Retained %d CPU values for %s: avg=%f / result=%f", len(usagesMHz), cluster.Reference().Value, avg, result)

		tags = maps.Clone(tags)
		tags["cpu"] = instanceTotal

		acc.AddFields("vsphere_cluster_cpu", map[string]any{"usage_average": result}, tags, t0)
	}

	return nil
}

func additionalHostMetrics(_ context.Context, _ *vim25.Client, hosts []*object.HostSystem, acc telegraf.Accumulator, h *Hierarchy, vmStatesPerHost map[string][]bool, t0 time.Time) error {
	for _, host := range hosts {
		moid := host.Reference().Value
		if vmStates, ok := vmStatesPerHost[moid]; ok {
			var running, stopped int

			for _, isRunning := range vmStates {
				if isRunning {
					running++
				} else {
					stopped++
				}
			}

			fields := map[string]any{
				"running_count": running,
				"stopped_count": stopped,
			}

			tags := map[string]string{
				"clustername": h.ParentClusterName(host),
				"dcname":      h.ParentDCName(host),
				"esxhostname": host.Name(),
				"moid":        moid,
			}

			acc.AddFields("vms", fields, tags, t0)
		}
	}

	return nil
}

func additionalVMMetrics(ctx context.Context, client *vim25.Client, vms []*object.VirtualMachine, cache *propsCache[vmLightProps], acc telegraf.Accumulator, h *Hierarchy, vmStatePerHost map[string][]bool, t0 time.Time) error {
	vmProps, err := retrieveProps(ctx, client, vms, relevantVMProperties, cache)
	if err != nil {
		return err
	}

	for vm, props := range vmProps {
		if props.Runtime.Host != nil {
			host := props.Runtime.Host.Value
			vmState := props.Runtime.PowerState == types.VirtualMachinePowerStatePoweredOn // true for running, false for stopped

			if states, ok := vmStatePerHost[host]; ok {
				vmStatePerHost[host] = append(states, vmState)
			} else {
				vmStatePerHost[host] = []bool{vmState}
			}
		}

		if props.Guest != nil {
			for _, disk := range props.Guest.Disk {
				usage := 100 - (float64(disk.FreeSpace)*100)/float64(disk.Capacity) // Percentage of disk used

				tags := map[string]string{
					"clustername": h.ParentClusterName(vm),
					"dcname":      h.ParentDCName(vm),
					"esxhostname": h.ParentHostName(vm),
					"item":        disk.DiskPath,
					"moid":        vm.Reference().Value,
					"vmname":      vm.Name(),
				}

				acc.AddFields("vsphere_vm_disk", map[string]any{"used_perc": usage}, tags, t0)
			}
		}
	}

	return nil
}
