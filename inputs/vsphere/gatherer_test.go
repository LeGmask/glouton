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
	"glouton/config"
	"glouton/facts"
	"glouton/prometheus/registry"
	"glouton/types"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func setupGathering(t *testing.T, dirName string) (mfs []*dto.MetricFamily, deferFn func()) {
	t.Helper()

	vSphereCfg, vSphereDeferFn := setupVSphereAPITest(t, dirName)
	ctx, cancel := context.WithTimeout(context.Background(), commonTimeout)
	deferFn = func() { cancel(); vSphereDeferFn() }

	manager := new(Manager)
	manager.RegisterGatherers(ctx, []config.VSphere{vSphereCfg}, func(opt registry.RegistrationOption, gatherer prometheus.Gatherer) (int, error) { return 0, nil }, nil, facts.NewMockFacter(make(map[string]string)))

	var (
		vSphere *vSphere
		ok      bool
	)

	u, _ := url.Parse(vSphereCfg.URL)
	if vSphere, ok = manager.vSpheres[u.Host]; !ok {
		deferFn()
		t.Fatalf("Expected manager to have a vSphere for the key %q.", u.Host)
	}

	manager.Devices(ctx, 0)

	t0 := time.Now().Truncate(time.Minute)

	realtimeMfs, err := vSphere.realtimeGatherer.GatherWithState(ctx, registry.GatherState{T0: t0, FromScrapeLoop: true})
	if err != nil {
		deferFn()
		t.Fatalf("Got an error gathering (%s) vSphere: %v", gatherRT, err)
	}

	/*histo5minMfs, err := vSphere.historical5minGatherer.GatherWithState(ctx, registry.GatherState{T0: t0, FromScrapeLoop: true})
	if err != nil {
		deferFn()
		t.Fatalf("Got an error gathering (%s) vSphere: %v", gatherHist5m, err)
	}*/

	histo30minMfs, err := vSphere.historical30minGatherer.GatherWithState(ctx, registry.GatherState{T0: t0, FromScrapeLoop: true})
	if err != nil {
		deferFn()
		t.Fatalf("Got an error gathering (%s) vSphere: %v", gatherHist30m, err)
	}

	mfs = append(realtimeMfs /*append(histo5minMfs, */, histo30minMfs... /*)...*/) //nolint: gocritic

	return mfs, deferFn
}

//nolint:nolintlint,gofmt, dupl
func TestGatheringESXI(t *testing.T) { //nolint:maintidx
	mfs, deferFn := setupGathering(t, "esxi_1")
	defer deferFn()

	expectedMfs := []*dto.MetricFamily{
		{
			Name: ptr("cpu_used"),
			Help: ptr(""),
			Type: dto.MetricType_UNTYPED.Enum(),
			Metric: []*dto.Metric{
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("10")},
						{Name: ptr("dcname"), Value: ptr("ha-datacenter")},
						{Name: ptr("esxhostname"), Value: ptr("esxi.test")},
						{Name: ptr("vmname"), Value: ptr("alp1")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("10")},
						{Name: ptr("dcname"), Value: ptr("ha-datacenter")},
						{Name: ptr("esxhostname"), Value: ptr("esxi.test")},
						{Name: ptr("vmname"), Value: ptr("alp1")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("ha-host")},
						{Name: ptr("dcname"), Value: ptr("ha-datacenter")},
						{Name: ptr("esxhostname"), Value: ptr("esxi.test")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.0)},
				},
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("ha-host")},
						{Name: ptr("dcname"), Value: ptr("ha-datacenter")},
						{Name: ptr("esxhostname"), Value: ptr("esxi.test")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
			},
		},
		{
			Name: ptr("cpu_usedmhz"),
			Help: ptr(""),
			Type: dto.MetricType_UNTYPED.Enum(),
			Metric: []*dto.Metric{
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("ha-host")},
						{Name: ptr("dcname"), Value: ptr("ha-datacenter")},
						{Name: ptr("esxhostname"), Value: ptr("esxi.test")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.0)},
				},
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("ha-host")},
						{Name: ptr("dcname"), Value: ptr("ha-datacenter")},
						{Name: ptr("esxhostname"), Value: ptr("esxi.test")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
			},
		},
		{
			Name: ptr("disk_used_perc"),
			Help: ptr(""),
			Type: dto.MetricType_UNTYPED.Enum(),
			Metric: []*dto.Metric{
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("10")},
						{Name: ptr("clustername"), Value: ptr("esxi.test")},
						{Name: ptr("dcname"), Value: ptr("ha-datacenter")},
						{Name: ptr("esxhostname"), Value: ptr("esxi.test")},
						{Name: ptr("item"), Value: ptr("/")},
						{Name: ptr("vmname"), Value: ptr("alp1")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("10")},
						{Name: ptr("clustername"), Value: ptr("esxi.test")},
						{Name: ptr("dcname"), Value: ptr("ha-datacenter")},
						{Name: ptr("esxhostname"), Value: ptr("esxi.test")},
						{Name: ptr("item"), Value: ptr("/boot")},
						{Name: ptr("vmname"), Value: ptr("alp1")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
			},
		},
		{
			Name: ptr("io_read_bytes"),
			Help: ptr(""),
			Type: dto.MetricType_UNTYPED.Enum(),
			Metric: []*dto.Metric{
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("10")},
						{Name: ptr("dcname"), Value: ptr("ha-datacenter")},
						{Name: ptr("disk"), Value: ptr("*")},
						{Name: ptr("esxhostname"), Value: ptr("esxi.test")},
						{Name: ptr("vmname"), Value: ptr("alp1")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("10")},
						{Name: ptr("dcname"), Value: ptr("ha-datacenter")},
						{Name: ptr("disk"), Value: ptr("*")},
						{Name: ptr("esxhostname"), Value: ptr("esxi.test")},
						{Name: ptr("vmname"), Value: ptr("alp1")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
			},
		},
		{
			Name: ptr("io_write_bytes"),
			Help: ptr(""),
			Type: dto.MetricType_UNTYPED.Enum(),
			Metric: []*dto.Metric{
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("10")},
						{Name: ptr("dcname"), Value: ptr("ha-datacenter")},
						{Name: ptr("disk"), Value: ptr("*")},
						{Name: ptr("esxhostname"), Value: ptr("esxi.test")},
						{Name: ptr("vmname"), Value: ptr("alp1")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("10")},
						{Name: ptr("dcname"), Value: ptr("ha-datacenter")},
						{Name: ptr("disk"), Value: ptr("*")},
						{Name: ptr("esxhostname"), Value: ptr("esxi.test")},
						{Name: ptr("vmname"), Value: ptr("alp1")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
			},
		},
		{
			Name: ptr("mem_total"),
			Help: ptr(""),
			Type: dto.MetricType_UNTYPED.Enum(),
			Metric: []*dto.Metric{
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("ha-host")},
						{Name: ptr("dcname"), Value: ptr("ha-datacenter")},
						{Name: ptr("esxhostname"), Value: ptr("esxi.test")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("ha-host")},
						{Name: ptr("dcname"), Value: ptr("ha-datacenter")},
						{Name: ptr("esxhostname"), Value: ptr("esxi.test")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
			},
		},
		{
			Name: ptr("mem_used_perc"),
			Help: ptr(""),
			Type: dto.MetricType_UNTYPED.Enum(),
			Metric: []*dto.Metric{
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("10")},
						{Name: ptr("dcname"), Value: ptr("ha-datacenter")},
						{Name: ptr("esxhostname"), Value: ptr("esxi.test")},
						{Name: ptr("vmname"), Value: ptr("alp1")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("10")},
						{Name: ptr("dcname"), Value: ptr("ha-datacenter")},
						{Name: ptr("esxhostname"), Value: ptr("esxi.test")},
						{Name: ptr("vmname"), Value: ptr("alp1")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("ha-host")},
						{Name: ptr("dcname"), Value: ptr("ha-datacenter")},
						{Name: ptr("esxhostname"), Value: ptr("esxi.test")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("ha-host")},
						{Name: ptr("dcname"), Value: ptr("ha-datacenter")},
						{Name: ptr("esxhostname"), Value: ptr("esxi.test")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
			},
		},
		{
			Name: ptr("net_bits_recv"),
			Help: ptr(""),
			Type: dto.MetricType_UNTYPED.Enum(),
			Metric: []*dto.Metric{
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("10")},
						{Name: ptr("dcname"), Value: ptr("ha-datacenter")},
						{Name: ptr("esxhostname"), Value: ptr("esxi.test")},
						{Name: ptr("interface"), Value: ptr("*")},
						{Name: ptr("vmname"), Value: ptr("alp1")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("10")},
						{Name: ptr("dcname"), Value: ptr("ha-datacenter")},
						{Name: ptr("esxhostname"), Value: ptr("esxi.test")},
						{Name: ptr("interface"), Value: ptr("*")},
						{Name: ptr("vmname"), Value: ptr("alp1")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("ha-host")},
						{Name: ptr("dcname"), Value: ptr("ha-datacenter")},
						{Name: ptr("esxhostname"), Value: ptr("esxi.test")},
						{Name: ptr("interface"), Value: ptr("*")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("ha-host")},
						{Name: ptr("dcname"), Value: ptr("ha-datacenter")},
						{Name: ptr("esxhostname"), Value: ptr("esxi.test")},
						{Name: ptr("interface"), Value: ptr("*")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
			},
		},
		{
			Name: ptr("net_bits_sent"),
			Help: ptr(""),
			Type: dto.MetricType_UNTYPED.Enum(),
			Metric: []*dto.Metric{
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("10")},
						{Name: ptr("dcname"), Value: ptr("ha-datacenter")},
						{Name: ptr("esxhostname"), Value: ptr("esxi.test")},
						{Name: ptr("interface"), Value: ptr("*")},
						{Name: ptr("vmname"), Value: ptr("alp1")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("10")},
						{Name: ptr("dcname"), Value: ptr("ha-datacenter")},
						{Name: ptr("esxhostname"), Value: ptr("esxi.test")},
						{Name: ptr("interface"), Value: ptr("*")},
						{Name: ptr("vmname"), Value: ptr("alp1")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("ha-host")},
						{Name: ptr("dcname"), Value: ptr("ha-datacenter")},
						{Name: ptr("esxhostname"), Value: ptr("esxi.test")},
						{Name: ptr("interface"), Value: ptr("*")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("ha-host")},
						{Name: ptr("dcname"), Value: ptr("ha-datacenter")},
						{Name: ptr("esxhostname"), Value: ptr("esxi.test")},
						{Name: ptr("interface"), Value: ptr("*")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
			},
		},
		{
			Name: ptr("vms_running_count"),
			Help: ptr(""),
			Type: dto.MetricType_UNTYPED.Enum(),
			Metric: []*dto.Metric{
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("ha-host")},
						{Name: ptr("clustername"), Value: ptr("esxi.test")},
						{Name: ptr("dcname"), Value: ptr("ha-datacenter")},
						{Name: ptr("esxhostname"), Value: ptr("esxi.test")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
			},
		},
		{
			Name: ptr("vms_stopped_count"),
			Help: ptr(""),
			Type: dto.MetricType_UNTYPED.Enum(),
			Metric: []*dto.Metric{
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("ha-host")},
						{Name: ptr("clustername"), Value: ptr("esxi.test")},
						{Name: ptr("dcname"), Value: ptr("ha-datacenter")},
						{Name: ptr("esxhostname"), Value: ptr("esxi.test")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
			},
		},
		{
			Name: ptr("vsphere_vm_cpu_latency_perc"),
			Help: ptr(""),
			Type: dto.MetricType_UNTYPED.Enum(),
			Metric: []*dto.Metric{
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("10")},
						{Name: ptr("dcname"), Value: ptr("ha-datacenter")},
						{Name: ptr("esxhostname"), Value: ptr("esxi.test")},
						{Name: ptr("vmname"), Value: ptr("alp1")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("10")},
						{Name: ptr("dcname"), Value: ptr("ha-datacenter")},
						{Name: ptr("esxhostname"), Value: ptr("esxi.test")},
						{Name: ptr("vmname"), Value: ptr("alp1")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
			},
		},
	}

	ignoreUnexported := cmpopts.IgnoreUnexported([]any{dto.MetricFamily{}, dto.Metric{}, dto.LabelPair{}, dto.Untyped{}}...)
	ignoreUntypedValue := cmpopts.IgnoreFields(dto.Untyped{}, "Value")
	ignoreTimestamp := cmpopts.IgnoreFields(dto.Metric{}, "TimestampMs")
	opts := cmp.Options{ignoreUnexported, ignoreUntypedValue, ignoreTimestamp, cmp.Comparer(vSphereLabelComparer)}

	if diff := cmp.Diff(expectedMfs, mfs, opts, makeVirtualDiskMetricComparer(opts)); diff != "" {
		t.Errorf("Unexpected metric families (-want +got):\n%s", diff)
	}
}

//nolint:nolintlint,gofmt, dupl
func TestGatheringVcsim(t *testing.T) { //nolint:maintidx
	mfs, deferFn := setupGathering(t, "vcenter_1")
	defer deferFn()

	expectedMfs := []*dto.MetricFamily{
		{
			Name: ptr("cpu_used"),
			Help: ptr(""),
			Type: dto.MetricType_UNTYPED.Enum(),
			Metric: []*dto.Metric{
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("domain-c16")},
						{Name: ptr("clustername"), Value: ptr("DC0_C0")},
						{Name: ptr("dcname"), Value: ptr("DC0")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("host-23")},
						{Name: ptr("clustername"), Value: ptr("DC0_C0")},
						{Name: ptr("dcname"), Value: ptr("DC0")},
						{Name: ptr("esxhostname"), Value: ptr("DC0_C0_H0")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("host-23")},
						{Name: ptr("clustername"), Value: ptr("DC0_C0")},
						{Name: ptr("dcname"), Value: ptr("DC0")},
						{Name: ptr("esxhostname"), Value: ptr("DC0_C0_H0")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("vm-28")},
						{Name: ptr("clustername"), Value: ptr("DC0_C0")},
						{Name: ptr("dcname"), Value: ptr("DC0")},
						{Name: ptr("esxhostname"), Value: ptr("DC0_C0_H0")},
						{Name: ptr("vmname"), Value: ptr("DC0_C0_RP0_VM0")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.0)},
				},
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("vm-28")},
						{Name: ptr("clustername"), Value: ptr("DC0_C0")},
						{Name: ptr("dcname"), Value: ptr("DC0")},
						{Name: ptr("esxhostname"), Value: ptr("DC0_C0_H0")},
						{Name: ptr("vmname"), Value: ptr("DC0_C0_RP0_VM0")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
			},
		},
		{
			Name: ptr("cpu_usedmhz"),
			Help: ptr(""),
			Type: dto.MetricType_UNTYPED.Enum(),
			Metric: []*dto.Metric{
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("host-23")},
						{Name: ptr("clustername"), Value: ptr("DC0_C0")},
						{Name: ptr("dcname"), Value: ptr("DC0")},
						{Name: ptr("esxhostname"), Value: ptr("DC0_C0_H0")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("host-23")},
						{Name: ptr("clustername"), Value: ptr("DC0_C0")},
						{Name: ptr("dcname"), Value: ptr("DC0")},
						{Name: ptr("esxhostname"), Value: ptr("DC0_C0_H0")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
			},
		},
		{
			Name: ptr("hosts_running_count"),
			Help: ptr(""),
			Type: dto.MetricType_UNTYPED.Enum(),
			Metric: []*dto.Metric{
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("domain-c16")},
						{Name: ptr("clustername"), Value: ptr("DC0_C0")},
						{Name: ptr("dcname"), Value: ptr("DC0")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
			},
		},
		{
			Name: ptr("hosts_stopped_count"),
			Help: ptr(""),
			Type: dto.MetricType_UNTYPED.Enum(),
			Metric: []*dto.Metric{
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("domain-c16")},
						{Name: ptr("clustername"), Value: ptr("DC0_C0")},
						{Name: ptr("dcname"), Value: ptr("DC0")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
			},
		},
		{
			Name: ptr("io_read_bytes"),
			Help: ptr(""),
			Type: dto.MetricType_UNTYPED.Enum(),
			Metric: []*dto.Metric{
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("vm-28")},
						{Name: ptr("clustername"), Value: ptr("DC0_C0")},
						{Name: ptr("dcname"), Value: ptr("DC0")},
						{Name: ptr("disk"), Value: ptr("*")},
						{Name: ptr("esxhostname"), Value: ptr("DC0_C0_H0")},
						{Name: ptr("vmname"), Value: ptr("DC0_C0_RP0_VM0")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("vm-28")},
						{Name: ptr("clustername"), Value: ptr("DC0_C0")},
						{Name: ptr("dcname"), Value: ptr("DC0")},
						{Name: ptr("disk"), Value: ptr("*")},
						{Name: ptr("esxhostname"), Value: ptr("DC0_C0_H0")},
						{Name: ptr("vmname"), Value: ptr("DC0_C0_RP0_VM0")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
			},
		},
		{
			Name: ptr("io_write_bytes"),
			Help: ptr(""),
			Type: dto.MetricType_UNTYPED.Enum(),
			Metric: []*dto.Metric{
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("vm-28")},
						{Name: ptr("clustername"), Value: ptr("DC0_C0")},
						{Name: ptr("dcname"), Value: ptr("DC0")},
						{Name: ptr("disk"), Value: ptr("*")},
						{Name: ptr("esxhostname"), Value: ptr("DC0_C0_H0")},
						{Name: ptr("vmname"), Value: ptr("DC0_C0_RP0_VM0")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("vm-28")},
						{Name: ptr("clustername"), Value: ptr("DC0_C0")},
						{Name: ptr("dcname"), Value: ptr("DC0")},
						{Name: ptr("disk"), Value: ptr("*")},
						{Name: ptr("esxhostname"), Value: ptr("DC0_C0_H0")},
						{Name: ptr("vmname"), Value: ptr("DC0_C0_RP0_VM0")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
			},
		},
		{
			Name: ptr("mem_total"),
			Help: ptr(""),
			Type: dto.MetricType_UNTYPED.Enum(),
			Metric: []*dto.Metric{
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("host-23")},
						{Name: ptr("clustername"), Value: ptr("DC0_C0")},
						{Name: ptr("dcname"), Value: ptr("DC0")},
						{Name: ptr("esxhostname"), Value: ptr("DC0_C0_H0")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("host-23")},
						{Name: ptr("clustername"), Value: ptr("DC0_C0")},
						{Name: ptr("dcname"), Value: ptr("DC0")},
						{Name: ptr("esxhostname"), Value: ptr("DC0_C0_H0")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
			},
		},
		{
			Name: ptr("mem_used_perc"),
			Help: ptr(""),
			Type: dto.MetricType_UNTYPED.Enum(),
			Metric: []*dto.Metric{
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("domain-c16")},
						{Name: ptr("clustername"), Value: ptr("DC0_C0")},
						{Name: ptr("dcname"), Value: ptr("DC0")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("host-23")},
						{Name: ptr("clustername"), Value: ptr("DC0_C0")},
						{Name: ptr("dcname"), Value: ptr("DC0")},
						{Name: ptr("esxhostname"), Value: ptr("DC0_C0_H0")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("host-23")},
						{Name: ptr("clustername"), Value: ptr("DC0_C0")},
						{Name: ptr("dcname"), Value: ptr("DC0")},
						{Name: ptr("esxhostname"), Value: ptr("DC0_C0_H0")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("vm-28")},
						{Name: ptr("clustername"), Value: ptr("DC0_C0")},
						{Name: ptr("dcname"), Value: ptr("DC0")},
						{Name: ptr("esxhostname"), Value: ptr("DC0_C0_H0")},
						{Name: ptr("vmname"), Value: ptr("DC0_C0_RP0_VM0")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("vm-28")},
						{Name: ptr("clustername"), Value: ptr("DC0_C0")},
						{Name: ptr("dcname"), Value: ptr("DC0")},
						{Name: ptr("esxhostname"), Value: ptr("DC0_C0_H0")},
						{Name: ptr("vmname"), Value: ptr("DC0_C0_RP0_VM0")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
			},
		},
		{
			Name: ptr("net_bits_recv"),
			Help: ptr(""),
			Type: dto.MetricType_UNTYPED.Enum(),
			Metric: []*dto.Metric{
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("host-23")},
						{Name: ptr("clustername"), Value: ptr("DC0_C0")},
						{Name: ptr("dcname"), Value: ptr("DC0")},
						{Name: ptr("esxhostname"), Value: ptr("DC0_C0_H0")},
						{Name: ptr("interface"), Value: ptr("*")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("host-23")},
						{Name: ptr("clustername"), Value: ptr("DC0_C0")},
						{Name: ptr("dcname"), Value: ptr("DC0")},
						{Name: ptr("esxhostname"), Value: ptr("DC0_C0_H0")},
						{Name: ptr("interface"), Value: ptr("*")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("vm-28")},
						{Name: ptr("clustername"), Value: ptr("DC0_C0")},
						{Name: ptr("dcname"), Value: ptr("DC0")},
						{Name: ptr("esxhostname"), Value: ptr("DC0_C0_H0")},
						{Name: ptr("interface"), Value: ptr("*")},
						{Name: ptr("vmname"), Value: ptr("DC0_C0_RP0_VM0")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("vm-28")},
						{Name: ptr("clustername"), Value: ptr("DC0_C0")},
						{Name: ptr("dcname"), Value: ptr("DC0")},
						{Name: ptr("esxhostname"), Value: ptr("DC0_C0_H0")},
						{Name: ptr("interface"), Value: ptr("*")},
						{Name: ptr("vmname"), Value: ptr("DC0_C0_RP0_VM0")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
			},
		},
		{
			Name: ptr("net_bits_sent"),
			Help: ptr(""),
			Type: dto.MetricType_UNTYPED.Enum(),
			Metric: []*dto.Metric{
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("host-23")},
						{Name: ptr("clustername"), Value: ptr("DC0_C0")},
						{Name: ptr("dcname"), Value: ptr("DC0")},
						{Name: ptr("esxhostname"), Value: ptr("DC0_C0_H0")},
						{Name: ptr("interface"), Value: ptr("*")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("host-23")},
						{Name: ptr("clustername"), Value: ptr("DC0_C0")},
						{Name: ptr("dcname"), Value: ptr("DC0")},
						{Name: ptr("esxhostname"), Value: ptr("DC0_C0_H0")},
						{Name: ptr("interface"), Value: ptr("*")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("vm-28")},
						{Name: ptr("clustername"), Value: ptr("DC0_C0")},
						{Name: ptr("dcname"), Value: ptr("DC0")},
						{Name: ptr("esxhostname"), Value: ptr("DC0_C0_H0")},
						{Name: ptr("interface"), Value: ptr("*")},
						{Name: ptr("vmname"), Value: ptr("DC0_C0_RP0_VM0")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("vm-28")},
						{Name: ptr("clustername"), Value: ptr("DC0_C0")},
						{Name: ptr("dcname"), Value: ptr("DC0")},
						{Name: ptr("esxhostname"), Value: ptr("DC0_C0_H0")},
						{Name: ptr("interface"), Value: ptr("*")},
						{Name: ptr("vmname"), Value: ptr("DC0_C0_RP0_VM0")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
			},
		},
		{
			Name: ptr("swap_out"),
			Help: ptr(""),
			Type: dto.MetricType_UNTYPED.Enum(),
			Metric: []*dto.Metric{
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("host-23")},
						{Name: ptr("clustername"), Value: ptr("DC0_C0")},
						{Name: ptr("dcname"), Value: ptr("DC0")},
						{Name: ptr("esxhostname"), Value: ptr("DC0_C0_H0")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("host-23")},
						{Name: ptr("clustername"), Value: ptr("DC0_C0")},
						{Name: ptr("dcname"), Value: ptr("DC0")},
						{Name: ptr("esxhostname"), Value: ptr("DC0_C0_H0")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
			},
		},
		{
			Name: ptr("swap_used"),
			Help: ptr(""),
			Type: dto.MetricType_UNTYPED.Enum(),
			Metric: []*dto.Metric{
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("vm-28")},
						{Name: ptr("clustername"), Value: ptr("DC0_C0")},
						{Name: ptr("dcname"), Value: ptr("DC0")},
						{Name: ptr("esxhostname"), Value: ptr("DC0_C0_H0")},
						{Name: ptr("vmname"), Value: ptr("DC0_C0_RP0_VM0")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("vm-28")},
						{Name: ptr("clustername"), Value: ptr("DC0_C0")},
						{Name: ptr("dcname"), Value: ptr("DC0")},
						{Name: ptr("esxhostname"), Value: ptr("DC0_C0_H0")},
						{Name: ptr("vmname"), Value: ptr("DC0_C0_RP0_VM0")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
			},
		},
		{
			Name: ptr("vms_running_count"),
			Help: ptr(""),
			Type: dto.MetricType_UNTYPED.Enum(),
			Metric: []*dto.Metric{
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("host-23")},
						{Name: ptr("clustername"), Value: ptr("DC0_C0")},
						{Name: ptr("dcname"), Value: ptr("DC0")},
						{Name: ptr("esxhostname"), Value: ptr("DC0_C0_H0")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
			},
		},
		{
			Name: ptr("vms_stopped_count"),
			Help: ptr(""),
			Type: dto.MetricType_UNTYPED.Enum(),
			Metric: []*dto.Metric{
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("host-23")},
						{Name: ptr("clustername"), Value: ptr("DC0_C0")},
						{Name: ptr("dcname"), Value: ptr("DC0")},
						{Name: ptr("esxhostname"), Value: ptr("DC0_C0_H0")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
			},
		},
		{
			Name: ptr("vsphere_vm_cpu_latency_perc"),
			Help: ptr(""),
			Type: dto.MetricType_UNTYPED.Enum(),
			Metric: []*dto.Metric{
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("vm-28")},
						{Name: ptr("clustername"), Value: ptr("DC0_C0")},
						{Name: ptr("dcname"), Value: ptr("DC0")},
						{Name: ptr("esxhostname"), Value: ptr("DC0_C0_H0")},
						{Name: ptr("vmname"), Value: ptr("DC0_C0_RP0_VM0")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
				{
					Label: []*dto.LabelPair{
						{Name: ptr("__meta_vsphere"), Value: ptr("127.0.0.1:xxxxx")},
						{Name: ptr("__meta_vsphere_moid"), Value: ptr("vm-28")},
						{Name: ptr("clustername"), Value: ptr("DC0_C0")},
						{Name: ptr("dcname"), Value: ptr("DC0")},
						{Name: ptr("esxhostname"), Value: ptr("DC0_C0_H0")},
						{Name: ptr("vmname"), Value: ptr("DC0_C0_RP0_VM0")},
					},
					Untyped: &dto.Untyped{Value: ptr(1.)},
				},
			},
		},
	}

	ignoreUnexported := cmpopts.IgnoreUnexported([]any{dto.MetricFamily{}, dto.Metric{}, dto.LabelPair{}, dto.Untyped{}}...)
	ignoreUntypedValue := cmpopts.IgnoreFields(dto.Untyped{}, "Value")
	ignoreTimestamp := cmpopts.IgnoreFields(dto.Metric{}, "TimestampMs")
	opts := cmp.Options{ignoreUnexported, ignoreUntypedValue, ignoreTimestamp, cmp.Comparer(vSphereLabelComparer)}

	if diff := cmp.Diff(expectedMfs, mfs, opts, makeVirtualDiskMetricComparer(opts)); diff != "" {
		t.Errorf("Unexpected metric families (-want +got):\n%s", diff)
	}
}

// vSphereLabelComparer handles the comparison between two "__meta_vsphere" labels,
// which is unpredictable because the port used by the simulator is random.
func vSphereLabelComparer(x, y *dto.LabelPair) bool {
	if x.GetName() == types.LabelMetaVSphere && y.GetName() == types.LabelMetaVSphere {
		xParts, yParts := strings.Split(x.GetValue(), ":"), strings.Split(y.GetValue(), ":")
		if len(xParts) != 2 || len(yParts) != 2 {
			return false
		}

		return xParts[0] == yParts[0]
	}

	return cmp.Equal(x, y, cmpopts.IgnoreUnexported(dto.LabelPair{}))
}

// makeVirtualDiskMetricComparer returns a comparer that tolerates having
// non-matching metrics within the "io_read_bytes" family.
// This can happen because the simulator sometimes seems to return 1 extra point,
// a minute in the past, but only for this particular metric ...
func makeVirtualDiskMetricComparer(opts []cmp.Option) cmp.Option {
	return cmp.Comparer(func(x, y *dto.MetricFamily) bool {
		if x.GetName() == "io_read_bytes" && y.GetName() == "io_read_bytes" {
			return cmp.Equal(x, y, append(opts, cmpopts.IgnoreFields(dto.MetricFamily{}, "Metric"))...)
		}

		return cmp.Equal(x, y, opts...)
	})
}
