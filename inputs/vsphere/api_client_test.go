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
	"crypto/tls"
	bleemeoTypes "glouton/bleemeo/types"
	"glouton/config"
	"glouton/prometheus/registry"
	"net/url"
	"sort"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/vmware/govmomi/simulator"
)

func setupVSphereAPITest(t *testing.T, dirName string) (vSphereCfg config.VSphere, deferFn func()) {
	t.Helper()

	model := simulator.VPX()

	err := model.Create()
	if err != nil {
		t.Fatal(err)
	}

	err = model.Load("testdata/" + dirName)
	if err != nil {
		t.Fatal(err)
	}

	model.Service.TLS = new(tls.Config)

	server := model.Service.NewServer()

	vSphereCfg = config.VSphere{
		URL:                server.URL.String(),
		Username:           "user",
		Password:           "pass",
		InsecureSkipVerify: true,
		MonitorVMs:         true,
	}

	return vSphereCfg, func() {
		model.Remove()
		server.Close()
	}
}

// TestVCenterDescribing list and describes the devices of a vCenter,
// by calling individually each method needed.
func TestVCenterDescribing(t *testing.T) {
	vSphereCfg, deferFn := setupVSphereAPITest(t, "vcenter_1")
	defer deferFn()

	vSphereURL, err := url.Parse(vSphereCfg.URL)
	if err != nil {
		t.Fatalf("Failed to parse vSphere URL %q: %v", vSphereCfg.URL, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	finder, err := newDeviceFinder(ctx, vSphereCfg)
	if err != nil {
		t.Fatal(err)
	}

	clusters, hosts, vms, err := findDevices(ctx, finder)
	if err != nil {
		t.Fatal(err)
	}

	fail := false

	if len(clusters) != 1 {
		t.Errorf("Expected 1 cluster, found %d.", len(clusters))

		fail = true
	}

	if len(hosts) != 1 {
		t.Errorf("Expected 1 host, found %d.", len(hosts))

		fail = true
	}

	if len(vms) != 1 {
		t.Errorf("Expected 1 VM, found %d.", len(vms))

		fail = true
	}

	if fail {
		t.FailNow()
	}

	dummyVSphere := newVSphere(vSphereCfg.URL, vSphereCfg, nil)
	deviceChan := make(chan bleemeoTypes.VSphereDevice, 1) // 1 of buffer because we only have 1 cluster, then 1 host, then 1 VM.

	dummyVSphere.describeClusters(ctx, clusters, deviceChan)

	if len(deviceChan) != 1 {
		t.Fatalf("Expected 1 cluster to be described, but got %d.", len(deviceChan))
	}

	devCluster := <-deviceChan

	cluster, ok := devCluster.(*Cluster)
	if !ok {
		t.Fatalf("Expected device to be a Cluster, but is %T.", devCluster)
	}

	expectedCluster := Cluster{
		device: device{
			source: vSphereURL.Host,
			moid:   "domain-c16",
			name:   "DC0_C0",
			facts: map[string]string{
				"cpu_cores": "2",
				"fqdn":      "DC0_C0",
			},
		},
		datastores: []string{"datastore-25"},
	}
	if diff := cmp.Diff(expectedCluster, *cluster, cmp.AllowUnexported(Cluster{}, device{})); diff != "" {
		t.Fatalf("Unexpected host description (-want +got):\n%s", diff)
	}

	dummyVSphere.describeHosts(ctx, hosts, deviceChan)

	if len(deviceChan) != 1 {
		t.Fatalf("Expected 1 host to be described, but got %d.", len(deviceChan))
	}

	devHost := <-deviceChan

	host, ok := devHost.(*HostSystem)
	if !ok {
		t.Fatalf("Expected device to be a HostSystem, but is %T.", devHost)
	}

	expectedHost := HostSystem{
		device{
			source: vSphereURL.Host,
			moid:   "host-23",
			name:   "DC0_C0_H0",
			facts: map[string]string{
				"cpu_cores":               "2",
				"cpu_model_name":          "Intel(R) Core(TM) i7-3615QM CPU @ 2.30GHz",
				"fqdn":                    "DC0_C0_H0",
				"hostname":                "DC0_C0_H0",
				"ipv6_enabled":            "false",
				"memory":                  "4.00 GB",
				"os_pretty_name":          "vmnix-x86",
				"primary_address":         "127.0.0.1",
				"product_name":            "VMware Virtual Platform",
				"system_vendor":           "VMware, Inc. (govmomi simulator)",
				"vsphere_host_version":    "6.5.0",
				"vsphere_vmotion_enabled": "false",
			},
		},
	}
	if diff := cmp.Diff(expectedHost, *host, cmp.AllowUnexported(HostSystem{}, device{})); diff != "" {
		t.Fatalf("Unexpected host description (-want +got):\n%s", diff)
	}

	dummyVSphere.describeVMs(ctx, vms, deviceChan)

	if len(deviceChan) != 1 {
		t.Fatalf("Expected 1 VM to be described, but got %d.", len(deviceChan))
	}

	devVM := <-deviceChan

	vm, ok := devVM.(*VirtualMachine)
	if !ok {
		t.Fatalf("Expected device to be a VirtualMachine, but is %T.", devVM)
	}

	expectedVM := VirtualMachine{
		device: device{
			source: vSphereURL.Host,
			moid:   "vm-28",
			name:   "DC0_C0_RP0_VM0",
			facts: map[string]string{
				"cpu_cores":             "1",
				"fqdn":                  "DC0_C0_RP0_VM0",
				"hostname":              "vm-28",
				"memory":                "32.00 MB",
				"vsphere_host":          "host-23",
				"vsphere_resource_pool": "resgroup-15",
				"vsphere_vm_name":       "DC0_C0_RP0_VM0",
				"vsphere_vm_version":    "vmx-13",
			},
			powerState: "poweredOn",
		},
		UUID:          "cd0681bf-2f18-5c00-9b9b-8197c0095348",
		inventoryPath: "/DC0/vm/DC0_C0_RP0_VM0",
	}
	if diff := cmp.Diff(expectedVM, *vm, cmp.AllowUnexported(VirtualMachine{}, device{})); diff != "" {
		t.Fatalf("Unexpected VM description (-want +got):\n%s", diff)
	}
}

// TestESXIDescribing lists and describes devices across multiple test cases,
// but by calling the higher-level method Manager.Devices.
func TestESXIDescribing(t *testing.T) { //nolint:maintidx
	testCases := []struct {
		name             string
		dirName          string
		expectedClusters []*Cluster
		expectedHosts    []*HostSystem
		expectedVMs      []*VirtualMachine
	}{
		{
			name:    "ESXI running on QEMU",
			dirName: "esxi_1",
			expectedHosts: []*HostSystem{
				{
					device{
						moid: "ha-host",
						name: "esxi.test",
						facts: map[string]string{
							"cpu_cores":               "4",
							"cpu_model_name":          "Intel(R) Core(TM) i7-8550U CPU @ 1.80GHz",
							"fqdn":                    "esxi.test",
							"hostname":                "esxi.test",
							"ipv6_enabled":            "false",
							"memory":                  "4.00 GB",
							"os_pretty_name":          "vmnix-x86",
							"primary_address":         "192.168.121.241",
							"product_name":            "Standard PC (i440FX + PIIX, 1996)",
							"system_vendor":           "QEMU",
							"timezone":                "UTC",
							"vsphere_host_version":    "8.0.1",
							"vsphere_vmotion_enabled": "false",
						},
						powerState: "poweredOn",
					},
				},
			},
			expectedVMs: []*VirtualMachine{
				{
					device: device{
						moid: "1",
						name: "1",
						facts: map[string]string{
							"fqdn":                  "1",
							"hostname":              "1",
							"vsphere_host":          "ha-host",
							"vsphere_resource_pool": "ha-root-pool",
						},
						powerState: "poweredOff",
					},
					inventoryPath: "/ha-datacenter/vm/vmfs/volumes/64d4b065-879077de-6dfb-5254000ed68e/vcenter/vcenter.vmx",
				},
				{
					device: device{
						moid: "4",
						name: "v-center",
						facts: map[string]string{
							"cpu_cores":             "2",
							"fqdn":                  "v-center",
							"hostname":              "4",
							"memory":                "14.00 GB",
							"os_pretty_name":        "Other 3.x or later Linux (64-bit)",
							"vsphere_datastore":     "datastore1",
							"vsphere_host":          "ha-host",
							"vsphere_resource_pool": "ha-root-pool",
							"vsphere_vm_name":       "v-center",
							"vsphere_vm_version":    "vmx-10",
						},
						powerState: "poweredOff",
					},
					UUID:          "564d8859-9d98-c670-0aca-009149c3a8af",
					inventoryPath: "/ha-datacenter/vm/v-center",
				},
				{
					device: device{
						moid: "7",
						name: "lunar",
						facts: map[string]string{
							"cpu_cores":             "2",
							"fqdn":                  "lunar",
							"hostname":              "7",
							"memory":                "1.00 GB",
							"os_pretty_name":        "Ubuntu Linux (64-bit)",
							"vsphere_datastore":     "datastore1",
							"vsphere_host":          "ha-host",
							"vsphere_resource_pool": "ha-root-pool",
							"vsphere_vm_name":       "lunar",
							"vsphere_vm_version":    "vmx-10",
						},
						powerState: "poweredOff",
					},
					UUID:          "564de3ab-988d-b51c-a5cb-5e1af6f5f313",
					inventoryPath: "/ha-datacenter/vm/lunar",
				},
			},
		},
		{
			name:    "vCenter vcsim'ulated",
			dirName: "vcenter_1",
			expectedClusters: []*Cluster{
				{
					device: device{
						moid: "domain-c16",
						name: "DC0_C0",
						facts: map[string]string{
							"cpu_cores": "2",
							"fqdn":      "DC0_C0",
						},
					},
					datastores: []string{"datastore-25"},
				},
			},
			expectedHosts: []*HostSystem{
				{
					device: device{
						moid: "host-23",
						name: "DC0_C0_H0",
						facts: map[string]string{
							"cpu_cores":               "2",
							"cpu_model_name":          "Intel(R) Core(TM) i7-3615QM CPU @ 2.30GHz",
							"fqdn":                    "DC0_C0_H0",
							"hostname":                "DC0_C0_H0",
							"ipv6_enabled":            "false",
							"memory":                  "4.00 GB",
							"os_pretty_name":          "vmnix-x86",
							"primary_address":         "127.0.0.1",
							"product_name":            "VMware Virtual Platform",
							"system_vendor":           "VMware, Inc. (govmomi simulator)",
							"vsphere_host_version":    "6.5.0",
							"vsphere_vmotion_enabled": "false",
						},
					},
				},
			},
			expectedVMs: []*VirtualMachine{
				{
					device: device{
						moid: "vm-28",
						name: "DC0_C0_RP0_VM0",
						facts: map[string]string{
							"cpu_cores":             "1",
							"fqdn":                  "DC0_C0_RP0_VM0",
							"hostname":              "vm-28",
							"memory":                "32.00 MB",
							"vsphere_host":          "host-23",
							"vsphere_resource_pool": "resgroup-15",
							"vsphere_vm_name":       "DC0_C0_RP0_VM0",
							"vsphere_vm_version":    "vmx-13",
						},
						powerState: "poweredOn",
					},
					UUID:          "cd0681bf-2f18-5c00-9b9b-8197c0095348",
					inventoryPath: "/DC0/vm/DC0_C0_RP0_VM0",
				},
			},
		},
		{
			name:          "vCenter",
			dirName:       "vcenter_2",
			expectedHosts: []*HostSystem{},
			expectedVMs: []*VirtualMachine{
				{
					device: device{
						moid: "vm-74",
						name: "app-haproxy2",
						facts: map[string]string{
							"cpu_cores":             "2",
							"fqdn":                  "app-haproxy2",
							"hostname":              "app-haproxy2",
							"memory":                "2.00 GB",
							"vsphere_host":          "host-11",                     // TODO improve this
							"vsphere_resource_pool": "resgroup-8",                  // TODO improve this
							"os_pretty_name":        "Debian GNU/Linux 5 (64-bit)", // TODO: we want "Debian GNU/Linux 11 (64-bit)"
							"primary_address":       "192.168.0.2",
							"vsphere_datastore":     "Datastore001",
							"vsphere_vm_name":       "app-haproxy2",
							"vsphere_vm_version":    "vmx-07",
						},
						powerState: "poweredOn",
					},
					UUID:          "d72b7d0e-95e3-4bd1-b788-18da89624595",
					inventoryPath: "/DC0/vm/app-haproxy2",
				},
			},
		},
	}

	for _, testCase := range testCases {
		tc := testCase

		t.Run(tc.name, func(t *testing.T) {
			// govmomi simulator doesn't seem to like having multiple instances in parallel.

			vSphereCfg, deferFn := setupVSphereAPITest(t, tc.dirName)
			defer deferFn()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			manager := new(Manager)
			manager.RegisterGatherers([]config.VSphere{vSphereCfg}, func(opt registry.RegistrationOption, gatherer prometheus.Gatherer) (int, error) { return 0, nil }, nil)

			devices := manager.Devices(ctx, 0)

			var (
				clusters []*Cluster
				hosts    []*HostSystem
				vms      []*VirtualMachine
			)

			for _, dev := range devices {
				switch kind := dev.Kind(); kind {
				case KindCluster:
					clusters = append(clusters, dev.(*Cluster)) //nolint: forcetypeassert
				case KindHost:
					hosts = append(hosts, dev.(*HostSystem)) //nolint: forcetypeassert
				case KindVM:
					vms = append(vms, dev.(*VirtualMachine)) //nolint: forcetypeassert
				default:
					// If this error is triggered, the test will (normally) stop at the if right below.
					t.Errorf("Unexpected device kind: %q", kind)
				}
			}

			if len(clusters) != len(tc.expectedClusters) || len(hosts) != len(tc.expectedHosts) || len(vms) != len(tc.expectedVMs) {
				t.Fatalf("Expected %d cluster(s), %d host(s) and %d VM(s), got %d cluster(s), %d host(s) and %d VM(s).",
					len(tc.expectedClusters), len(tc.expectedHosts), len(tc.expectedVMs), len(clusters), len(hosts), len(vms))
			}

			noSourceCmp := cmpopts.IgnoreFields(device{}, "source")

			sortDevices(tc.expectedClusters)
			sortDevices(clusters)
			// We need to compare the clusters, hosts and VMs one by one, otherwise the diff is way harder to analyze.
			for i, expectedCluster := range tc.expectedClusters {
				if diff := cmp.Diff(expectedCluster, clusters[i], cmp.AllowUnexported(Cluster{}, device{}), noSourceCmp); diff != "" {
					t.Errorf("Unexpected cluster description (-want +got):\n%s", diff)
				}
			}

			sortDevices(tc.expectedHosts)
			sortDevices(hosts)
			for i, expectedHost := range tc.expectedHosts {
				if diff := cmp.Diff(expectedHost, hosts[i], cmp.AllowUnexported(HostSystem{}, device{}), noSourceCmp); diff != "" {
					t.Errorf("Unexpected host description (-want +got):\n%s", diff)
				}
			}

			sortDevices(tc.expectedVMs)
			sortDevices(vms)

			for i, expectedVM := range tc.expectedVMs {
				if diff := cmp.Diff(expectedVM, vms[i], cmp.AllowUnexported(VirtualMachine{}, device{}), noSourceCmp); diff != "" {
					t.Errorf("Unexpected VM description (-want +got):\n%s", diff)
				}
			}
		})
	}
}

// sortDevices sorts the given VSphereDevice slice in place, according to device MOIDs.
func sortDevices[D bleemeoTypes.VSphereDevice](devices []D) {
	sort.Slice(devices, func(i, j int) bool {
		return devices[i].MOID() < devices[j].MOID()
	})
}
