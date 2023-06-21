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

package discovery

import (
	"context"
	"errors"
	"fmt"
	"glouton/agent/state"
	"glouton/config"
	"glouton/facts"
	"glouton/prometheus/registry"
	"glouton/types"
	"reflect"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/influxdata/telegraf"
)

var (
	errNotImplemented = errors.New("not implemented")
	errNotCalled      = errors.New("not called, want call")
	errWantName       = errors.New("want name")
)

type mockState struct {
	DiscoveredService []Service
}

func (ms mockState) Set(key string, object interface{}) error {
	_ = key
	_ = object

	return errNotImplemented
}

func (ms mockState) Get(key string, object interface{}) error {
	_ = key

	if services, ok := object.(*[]Service); ok {
		*services = ms.DiscoveredService

		return nil
	}

	return errNotImplemented
}

type mockCollector struct {
	ExpectedAddedName string
	NewID             int
	ExpectedRemoveID  int
	err               error
}

func (m *mockCollector) AddInput(_ telegraf.Input, name string) (int, error) {
	if name != m.ExpectedAddedName {
		m.err = fmt.Errorf("AddInput(_, %s), %w=%s", name, errWantName, m.ExpectedAddedName)

		return 0, m.err
	}

	m.ExpectedAddedName = ""

	return m.NewID, nil
}

func (m *mockCollector) RemoveInput(id int) {
	if id != m.ExpectedRemoveID {
		m.err = fmt.Errorf("RemoveInput(%d), %w=%d", id, errWantName, m.ExpectedRemoveID)

		return
	}

	m.ExpectedRemoveID = 0
}

func (m *mockCollector) ExpectationFullified() error {
	if m.err != nil {
		return m.err
	}

	if m.ExpectedAddedName != "" {
		return fmt.Errorf("AddInput() %w with name=%s", errNotCalled, m.ExpectedAddedName)
	}

	if m.ExpectedRemoveID != 0 {
		return fmt.Errorf("RemoveInput() %w with id=%d", errNotCalled, m.ExpectedRemoveID)
	}

	return nil
}

// Test dynamic Discovery with single service present.
func TestDiscoverySingle(t *testing.T) {
	t0 := time.Now()

	cases := []struct {
		dynamicResult   Service
		previousService Service
		want            Service
	}{
		{
			previousService: Service{},
			dynamicResult: Service{
				Name:            "memcached",
				ServiceType:     MemcachedService,
				ContainerID:     "",
				ListenAddresses: []facts.ListenAddress{{NetworkFamily: "tcp", Address: "127.0.0.1", Port: 11211}},
				IPAddress:       "127.0.0.1",
				HasNetstatInfo:  true,
				LastNetstatInfo: t0,
			},
			want: Service{
				Name:            "memcached",
				ServiceType:     MemcachedService,
				ContainerID:     "",
				ListenAddresses: []facts.ListenAddress{{NetworkFamily: "tcp", Address: "127.0.0.1", Port: 11211}},
				IPAddress:       "127.0.0.1",
				HasNetstatInfo:  true,
				LastNetstatInfo: t0,
			},
		},
		{
			previousService: Service{
				Name:            "memcached",
				ServiceType:     MemcachedService,
				ContainerID:     "",
				ListenAddresses: []facts.ListenAddress{{NetworkFamily: "tcp", Address: "127.0.0.1", Port: 11211}},
				IPAddress:       "127.0.0.1",
				HasNetstatInfo:  true,
				LastNetstatInfo: t0,
			},
			dynamicResult: Service{
				Name:            "memcached",
				ServiceType:     MemcachedService,
				ContainerID:     "",
				ListenAddresses: []facts.ListenAddress{{NetworkFamily: "tcp", Address: "127.0.0.1", Port: 11211}},
				IPAddress:       "127.0.0.1",
				HasNetstatInfo:  true,
				LastNetstatInfo: t0,
			},
			want: Service{
				Name:            "memcached",
				ServiceType:     MemcachedService,
				ContainerID:     "",
				ListenAddresses: []facts.ListenAddress{{NetworkFamily: "tcp", Address: "127.0.0.1", Port: 11211}},
				IPAddress:       "127.0.0.1",
				HasNetstatInfo:  true,
				LastNetstatInfo: t0,
			},
		},
		{
			previousService: Service{
				Name:            "memcached",
				ServiceType:     MemcachedService,
				ContainerID:     "",
				ListenAddresses: []facts.ListenAddress{{NetworkFamily: "tcp", Address: "10.0.0.5", Port: 11211}},
				IPAddress:       "10.0.0.5",
				HasNetstatInfo:  true,
				LastNetstatInfo: t0,
			},
			dynamicResult: Service{
				Name:            "memcached",
				ServiceType:     MemcachedService,
				ContainerID:     "",
				ListenAddresses: []facts.ListenAddress{{NetworkFamily: "tcp", Address: "127.0.0.1", Port: 11211}},
				IPAddress:       "",
				HasNetstatInfo:  false,
			},
			want: Service{
				Name:            "memcached",
				ServiceType:     MemcachedService,
				ContainerID:     "",
				ListenAddresses: []facts.ListenAddress{{NetworkFamily: "tcp", Address: "10.0.0.5", Port: 11211}},
				IPAddress:       "10.0.0.5",
				HasNetstatInfo:  true,
				LastNetstatInfo: t0,
			},
		},
		{
			previousService: Service{
				Name:            "memcached",
				ServiceType:     MemcachedService,
				ContainerID:     "",
				ListenAddresses: []facts.ListenAddress{{NetworkFamily: "tcp", Address: "10.0.0.5", Port: 11211}},
				IPAddress:       "10.0.0.5",
				HasNetstatInfo:  true,
				LastNetstatInfo: t0,
			},
			dynamicResult: Service{
				Name:            "memcached",
				ServiceType:     MemcachedService,
				ContainerID:     "",
				ListenAddresses: []facts.ListenAddress{{NetworkFamily: "tcp", Address: "127.0.0.1", Port: 11211}},
				IPAddress:       "127.0.0.1",
				HasNetstatInfo:  true,
			},
			want: Service{
				Name:            "memcached",
				ServiceType:     MemcachedService,
				ContainerID:     "",
				ListenAddresses: []facts.ListenAddress{{NetworkFamily: "tcp", Address: "127.0.0.1", Port: 11211}},
				IPAddress:       "127.0.0.1",
				HasNetstatInfo:  true,
				LastNetstatInfo: t0,
			},
		},
	}

	ctx := context.Background()

	for i, c := range cases {
		var previousService []Service

		if c.previousService.ServiceType != "" {
			previousService = append(previousService, c.previousService)
		}

		state := mockState{
			DiscoveredService: previousService,
		}
		disc, _ := New(&MockDiscoverer{result: []Service{c.dynamicResult}}, nil, nil, state, mockContainerInfo{}, nil, nil, nil, facts.ContainerFilter{}.ContainerIgnored, types.MetricFormatBleemeo, nil)

		srv, err := disc.Discovery(ctx, 0)
		if err != nil {
			t.Error(err)
		}

		if len(srv) != 1 {
			t.Errorf("Case #%d: len(srv) == %v, want 1", i, len(srv))
		}

		if srv[0].Name != c.want.Name {
			t.Errorf("Case #%d: Name == %#v, want %#v", i, srv[0].Name, c.want.Name)
		}

		if srv[0].ServiceType != c.want.ServiceType {
			t.Errorf("Case #%d: ServiceType == %#v, want %#v", i, srv[0].ServiceType, c.want.ServiceType)
		}

		if srv[0].ContainerID != c.want.ContainerID {
			t.Errorf("Case #%d: ContainerID == %#v, want %#v", i, srv[0].ContainerID, c.want.ContainerID)
		}

		if srv[0].IPAddress != c.want.IPAddress {
			t.Errorf("Case #%d: IPAddress == %#v, want %#v", i, srv[0].IPAddress, c.want.IPAddress)
		}

		if !reflect.DeepEqual(srv[0].ListenAddresses, c.want.ListenAddresses) {
			t.Errorf("Case #%d: ListenAddresses == %v, want %v", i, srv[0].ListenAddresses, c.want.ListenAddresses)
		}

		if srv[0].HasNetstatInfo != c.want.HasNetstatInfo {
			t.Errorf("Case #%d: hasNetstatInfo == %#v, want %#v", i, srv[0].HasNetstatInfo, c.want.HasNetstatInfo)
		}
	}
}

func Test_applyOverride(t *testing.T) { //nolint:maintidx
	type args struct {
		discoveredServicesMap map[NameInstance]Service
		servicesOverride      map[NameInstance]config.Service
	}

	tests := []struct {
		name string
		args args
		want map[NameInstance]Service
	}{
		{
			name: "empty",
			args: args{
				discoveredServicesMap: nil,
				servicesOverride:      nil,
			},
			want: make(map[NameInstance]Service),
		},
		{
			name: "no override",
			args: args{
				discoveredServicesMap: map[NameInstance]Service{
					{Name: "apache"}: {
						Name:            "apache",
						ServiceType:     ApacheService,
						IPAddress:       "127.0.0.1",
						ListenAddresses: []facts.ListenAddress{},
					},
				},
				servicesOverride: nil,
			},
			want: map[NameInstance]Service{
				{Name: "apache"}: {
					Name:            "apache",
					ServiceType:     ApacheService,
					IPAddress:       "127.0.0.1",
					ListenAddresses: []facts.ListenAddress{},
				},
			},
		},
		{
			name: "address override",
			args: args{
				discoveredServicesMap: map[NameInstance]Service{
					{Name: "apache"}: {
						Name:        "apache",
						ServiceType: ApacheService,
					},
				},
				servicesOverride: map[NameInstance]config.Service{
					{Name: "apache"}: {
						Address: "10.0.1.2",
					},
				},
			},
			want: map[NameInstance]Service{
				{Name: "apache"}: {
					Name:        "apache",
					ServiceType: ApacheService,
					Config: config.Service{
						Address: "10.0.1.2",
					},
					ListenAddresses: []facts.ListenAddress{
						{
							NetworkFamily: "tcp",
							Address:       "10.0.1.2",
							Port:          80,
						},
					},
					IPAddress: "10.0.1.2",
				},
			},
		},
		{
			name: "add custom check",
			args: args{
				discoveredServicesMap: map[NameInstance]Service{
					{Name: "apache"}: {
						Name:        "apache",
						ServiceType: ApacheService,
					},
				},
				servicesOverride: map[NameInstance]config.Service{
					{Name: "myapplication"}: {
						Port:         8080,
						CheckType:    customCheckNagios,
						CheckCommand: "command-to-run",
					},
					{Name: "custom_webserver"}: {
						Port: 8081,
					},
				},
			},
			want: map[NameInstance]Service{
				{Name: "apache"}: {
					Name:        "apache",
					ServiceType: ApacheService,
				},
				{Name: "myapplication"}: {
					ServiceType: CustomService,
					Config: config.Service{
						Address:      "127.0.0.1", // default as soon as port is set
						Port:         8080,
						CheckType:    customCheckNagios,
						CheckCommand: "command-to-run",
					},
					Name:   "myapplication",
					Active: true,
				},
				{Name: "custom_webserver"}: {
					ServiceType: CustomService,
					Config: config.Service{
						Address:   "127.0.0.1", // default as soon as port is set
						Port:      8081,
						CheckType: customCheckTCP, // default as soon as port is set,
					},
					Name:   "custom_webserver",
					Active: true,
				},
			},
		},
		{
			name: "bad custom check",
			args: args{
				discoveredServicesMap: nil,
				servicesOverride: map[NameInstance]config.Service{
					{Name: "myapplication"}: { // the check_command is missing
						Port:      8080,
						CheckType: customCheckNagios,
					},
					{Name: "custom_webserver"}: { // port is missing
						CheckType: customCheckHTTP,
					},
				},
			},
			want: map[NameInstance]Service{},
		},
		{
			name: "ignore ports",
			args: args{
				discoveredServicesMap: map[NameInstance]Service{
					{Name: "apache"}: {
						Name:        "apache",
						ServiceType: ApacheService,
						IPAddress:   "127.0.0.1",
						ListenAddresses: []facts.ListenAddress{
							{NetworkFamily: "tcp", Address: "127.0.0.1", Port: 80},
							{NetworkFamily: "tcp", Address: "127.0.0.1", Port: 443},
						},
					},
				},
				servicesOverride: map[NameInstance]config.Service{
					{Name: "apache"}: {
						IgnorePorts: []int{443, 22},
					},
				},
			},
			want: map[NameInstance]Service{
				{Name: "apache"}: {
					Name:        "apache",
					ServiceType: ApacheService,
					IPAddress:   "127.0.0.1",
					ListenAddresses: []facts.ListenAddress{
						{NetworkFamily: "tcp", Address: "127.0.0.1", Port: 80},
						// It's not applyOverride which remove ignored ports
						{NetworkFamily: "tcp", Address: "127.0.0.1", Port: 443},
					},
					IgnoredPorts: map[int]bool{
						22:  true,
						443: true,
					},
					Config: config.Service{
						IgnorePorts: []int{443, 22},
					},
				},
			},
		},
		{
			name: "ignore ports with space",
			args: args{
				discoveredServicesMap: map[NameInstance]Service{
					{Name: "apache"}: {
						Name:        "apache",
						ServiceType: ApacheService,
					},
				},
				servicesOverride: map[NameInstance]config.Service{
					{Name: "apache"}: {
						IgnorePorts: []int{443, 22},
					},
				},
			},
			want: map[NameInstance]Service{
				{Name: "apache"}: {
					Name:        "apache",
					ServiceType: ApacheService,
					IgnoredPorts: map[int]bool{
						22:  true,
						443: true,
					},
					Config: config.Service{
						IgnorePorts: []int{443, 22},
					},
				},
			},
		},
		{
			name: "override stack",
			args: args{
				discoveredServicesMap: map[NameInstance]Service{
					{Name: "apache"}: {
						Name:        "apache",
						ServiceType: ApacheService,
					},
				},
				servicesOverride: map[NameInstance]config.Service{
					{Name: "apache"}: {
						Stack: "website",
					},
				},
			},
			want: map[NameInstance]Service{
				{Name: "apache"}: {
					Name:        "apache",
					ServiceType: ApacheService,
					Stack:       "website",
					Config: config.Service{
						Stack: "website",
					},
				},
			},
		},
		{
			name: "no override stack",
			args: args{
				discoveredServicesMap: map[NameInstance]Service{
					{Name: "apache"}: {
						Name:        "apache",
						ServiceType: ApacheService,
						Stack:       "website",
					},
				},
				servicesOverride: map[NameInstance]config.Service{
					{Name: "apache"}: {
						Stack: "",
					},
				},
			},
			want: map[NameInstance]Service{
				{Name: "apache"}: {
					Name:        "apache",
					ServiceType: ApacheService,
					Stack:       "website",
					Config: config.Service{
						Stack: "",
					},
				},
			},
		},
		{
			name: "override port from jmx port",
			args: args{
				discoveredServicesMap: map[NameInstance]Service{},
				servicesOverride: map[NameInstance]config.Service{
					{Name: "jmx_custom"}: {
						ID:      "jmx_custom",
						JMXPort: 1000,
					},
				},
			},
			want: map[NameInstance]Service{
				{Name: "jmx_custom"}: {
					Name:        "jmx_custom",
					ServiceType: CustomService,
					Config: config.Service{
						ID:        "jmx_custom",
						Address:   "127.0.0.1",
						Port:      1000,
						JMXPort:   1000,
						CheckType: customCheckTCP,
					},
					Active: true,
				},
			},
		},
		{
			name: "no override port from jmx port",
			args: args{
				discoveredServicesMap: map[NameInstance]Service{},
				servicesOverride: map[NameInstance]config.Service{
					{Name: "jmx_custom"}: {
						ID:      "jmx_custom",
						Port:    8000,
						JMXPort: 1000,
					},
				},
			},
			want: map[NameInstance]Service{
				{Name: "jmx_custom"}: {
					Name:        "jmx_custom",
					ServiceType: CustomService,
					Config: config.Service{
						ID:        "jmx_custom",
						Address:   "127.0.0.1",
						Port:      8000,
						JMXPort:   1000,
						CheckType: customCheckTCP,
					},
					Active: true,
				},
			},
		},
		{
			name: "override docker labels",
			args: args{
				discoveredServicesMap: map[NameInstance]Service{
					{Name: "kafka"}: {
						Name:        "kafka",
						ServiceType: KafkaService,
						// This case happens when "glouton.port" and
						// "glouton.jmx_port" docker labels are set.
						Config: config.Service{
							Port:    8000,
							JMXPort: 1000,
						},
					},
				},
				servicesOverride: map[NameInstance]config.Service{
					{Name: "kafka"}: {
						ID:      "kafka",
						Port:    9000,
						JMXPort: 2000,
					},
				},
			},
			want: map[NameInstance]Service{
				{Name: "kafka"}: {
					Name:        "kafka",
					ServiceType: KafkaService,
					Config: config.Service{
						ID:      "kafka",
						Port:    9000,
						JMXPort: 2000,
					},
				},
			},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got := applyOverride(tt.args.discoveredServicesMap, tt.args.servicesOverride)
			if diff := cmp.Diff(tt.want, got, cmpopts.IgnoreUnexported(Service{})); diff != "" {
				t.Errorf("applyOverride diff:\n %s", diff)
			}
		})
	}
}

func TestUpdateMetricsAndCheck(t *testing.T) {
	fakeCollector := &mockCollector{
		ExpectedAddedName: "nginx",
		NewID:             42,
	}
	mockDynamic := &MockDiscoverer{}
	docker := mockContainerInfo{
		containers: map[string]facts.FakeContainer{
			"1234": {},
		},
	}
	state := mockState{}

	reg, err := registry.New(registry.Option{})
	if err != nil {
		t.Fatal(err)
	}

	disc, _ := New(mockDynamic, fakeCollector, reg, state, nil, nil, nil, nil, facts.ContainerFilter{}.ContainerIgnored, types.MetricFormatBleemeo, nil)
	disc.containerInfo = docker

	mockDynamic.result = []Service{
		{
			Name:            "nginx",
			Instance:        "nginx1",
			ServiceType:     NginxService,
			Active:          true,
			ContainerID:     "1234",
			ContainerName:   "nginx1",
			IPAddress:       "172.16.0.2",
			ListenAddresses: []facts.ListenAddress{{NetworkFamily: "tcp", Address: "172.16.0.2", Port: 80}},
		},
	}

	if _, err := disc.Discovery(context.Background(), 0); err != nil {
		t.Error(err)
	}

	if err := fakeCollector.ExpectationFullified(); err != nil {
		t.Error(err)
	}

	mockDynamic.result = []Service{
		{
			Name:            "nginx",
			Instance:        "nginx1",
			ServiceType:     NginxService,
			Active:          true,
			ContainerID:     "1234",
			ContainerName:   "nginx1",
			IPAddress:       "172.16.0.2",
			ListenAddresses: []facts.ListenAddress{{NetworkFamily: "tcp", Address: "172.16.0.2", Port: 80}},
		},
		{
			Name:            "memcached",
			Instance:        "",
			ServiceType:     MemcachedService,
			Active:          true,
			IPAddress:       "127.0.0.1",
			ListenAddresses: []facts.ListenAddress{{NetworkFamily: "tcp", Address: "127.0.0.1", Port: 11211}},
		},
	}
	fakeCollector.ExpectedAddedName = "memcached"
	fakeCollector.NewID = 1337

	if _, err := disc.Discovery(context.Background(), 0); err != nil {
		t.Error(err)
	}

	if err := fakeCollector.ExpectationFullified(); err != nil {
		t.Error(err)
	}

	docker.containers = map[string]facts.FakeContainer{
		"1239": {},
	}
	mockDynamic.result = []Service{
		{
			Name:            "nginx",
			Instance:        "nginx1",
			ServiceType:     NginxService,
			Active:          true,
			ContainerID:     "1239",
			ContainerName:   "nginx1",
			IPAddress:       "172.16.0.2",
			ListenAddresses: []facts.ListenAddress{{NetworkFamily: "tcp", Address: "172.16.0.2", Port: 80}},
		},
		{
			Name:            "memcached",
			Instance:        "",
			ServiceType:     MemcachedService,
			Active:          true,
			IPAddress:       "127.0.0.1",
			ListenAddresses: []facts.ListenAddress{{NetworkFamily: "tcp", Address: "127.0.0.1", Port: 11211}},
		},
	}
	fakeCollector.ExpectedAddedName = "nginx"
	fakeCollector.NewID = 9999
	fakeCollector.ExpectedRemoveID = 42

	if _, err := disc.Discovery(context.Background(), 0); err != nil {
		t.Error(err)
	}

	if err := fakeCollector.ExpectationFullified(); err != nil {
		t.Error(err)
	}
}

func Test_usePreviousNetstat(t *testing.T) {
	t0 := time.Now()

	tests := []struct {
		name            string
		now             time.Time
		previousService Service
		newService      Service
		want            bool
	}{
		{
			name: "service restarted",
			now:  t0,
			previousService: Service{
				Name:            "nginx",
				ContainerID:     "",
				HasNetstatInfo:  true,
				LastNetstatInfo: t0.Add(-30 * time.Minute),
			},
			newService: Service{
				Name:            "nginx",
				ContainerID:     "",
				HasNetstatInfo:  false,
				LastNetstatInfo: time.Time{},
			},
			want: true,
		},
		{
			name: "service restarted, netstat available",
			now:  t0,
			previousService: Service{
				Name:            "nginx",
				ContainerID:     "",
				HasNetstatInfo:  true,
				LastNetstatInfo: t0.Add(-30 * time.Minute),
			},
			newService: Service{
				Name:            "nginx",
				ContainerID:     "",
				HasNetstatInfo:  true,
				LastNetstatInfo: t0,
			},
			want: false,
		},
		{
			name: "missing LastNetstatInfo on previous",
			now:  t0,
			previousService: Service{
				Name:            "nginx",
				ContainerID:     "",
				HasNetstatInfo:  true,
				LastNetstatInfo: time.Time{},
			},
			newService: Service{
				Name:            "nginx",
				ContainerID:     "",
				HasNetstatInfo:  false,
				LastNetstatInfo: time.Time{},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		tt := tt

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := usePreviousNetstat(tt.now, tt.previousService, tt.newService); got != tt.want {
				t.Errorf("usePreviousNetstat() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateServices(t *testing.T) {
	services := []config.Service{
		{
			ID:       "apache",
			Instance: "",
			Port:     80,
			Address:  "127.0.0.1",
			HTTPPath: "/",
			HTTPHost: "127.0.0.1:80",
		},
		{
			ID:       "apache",
			Instance: "",
			Port:     81,
			Address:  "127.0.0.1",
			HTTPPath: "/",
			HTTPHost: "127.0.0.1:80",
		},
		{
			ID:       "apache",
			Instance: "CONTAINER_NAME",
			Port:     80,
			Address:  "127.17.0.2",
			HTTPPath: "/",
			HTTPHost: "127.0.0.1:80",
		},
		{
			ID:       "apache",
			Instance: "CONTAINER_NAME",
			Port:     81,
			Address:  "127.17.0.2",
			HTTPPath: "/",
			HTTPHost: "127.0.0.1:80",
		},
		{
			CheckType:    "nagios",
			CheckCommand: "azerty",
		},
		{
			ID:           "myapplication",
			Port:         80,
			CheckType:    "nagios",
			CheckCommand: "command-to-run",
		},
		{
			ID:           " not fixable@",
			CheckType:    "nagios",
			CheckCommand: "azerty",
		},
		{
			ID:        "custom_webserver",
			Port:      8181,
			CheckType: "http",
		},
		{
			ID:           "custom-bad.name",
			CheckType:    "nagios",
			CheckCommand: "azerty",
		},
		{
			ID:       "ssl_and_starttls",
			SSL:      true,
			StartTLS: true,
		},
		{
			ID:            "good_stats_protocol",
			StatsProtocol: "http",
		},
		{
			ID:            "bad_stats_protocol",
			StatsProtocol: "bad",
		},
	}

	wantWarnings := []string{
		"invalid config value: a service override is duplicated for 'apache'",
		"invalid config value: a service override is duplicated for 'apache' on instance 'CONTAINER_NAME'",
		"invalid config value: a key \"id\" is missing in one of your service override",
		"invalid config value: service id \" not fixable@\" can only contains letters, digits and underscore",
		"invalid config value: service id \"custom-bad.name\" can not contains dot (.) or dash (-). Changed to \"custom_bad_name\"",
		"invalid config value: service 'ssl_and_starttls' can't set both SSL and StartTLS, StartTLS will be used",
		"invalid config value: service 'bad_stats_protocol' has an unsupported stats protocol: 'bad'",
	}

	wantServices := map[NameInstance]config.Service{
		{
			Name:     "apache",
			Instance: "",
		}: {
			ID:       "apache",
			Instance: "",
			Port:     81,
			Address:  "127.0.0.1",
			HTTPPath: "/",
			HTTPHost: "127.0.0.1:80",
		},
		{
			Name:     "apache",
			Instance: "CONTAINER_NAME",
		}: {
			ID:       "apache",
			Instance: "CONTAINER_NAME",
			Port:     81,
			Address:  "127.17.0.2",
			HTTPPath: "/",
			HTTPHost: "127.0.0.1:80",
		},
		{
			Name:     "myapplication",
			Instance: "",
		}: {
			ID:           "myapplication",
			Port:         80,
			CheckType:    "nagios",
			CheckCommand: "command-to-run",
		},
		{
			Name:     "custom_webserver",
			Instance: "",
		}: {
			ID:        "custom_webserver",
			Port:      8181,
			CheckType: "http",
		},
		{
			Name:     "custom_bad_name",
			Instance: "",
		}: {
			ID:           "custom_bad_name",
			CheckType:    "nagios",
			CheckCommand: "azerty",
		},
		{
			Name: "ssl_and_starttls",
		}: {
			ID:       "ssl_and_starttls",
			SSL:      false,
			StartTLS: true,
		},
		{
			Name: "good_stats_protocol",
		}: {
			ID:            "good_stats_protocol",
			StatsProtocol: "http",
		},
		{
			Name: "bad_stats_protocol",
		}: {
			ID:            "bad_stats_protocol",
			StatsProtocol: "",
		},
	}

	gotServices, gotWarnings := validateServices(services)

	if diff := cmp.Diff(gotServices, wantServices); diff != "" {
		t.Fatalf("Validate returned unexpected services:\n%s", diff)
	}

	gotWarningsStr := make([]string, 0, len(gotWarnings))
	for _, warning := range gotWarnings {
		gotWarningsStr = append(gotWarningsStr, warning.Error())
	}

	if diff := cmp.Diff(gotWarningsStr, wantWarnings); diff != "" {
		t.Fatalf("Validate returned unexpected warnings:\n%s", diff)
	}
}

func Test_servicesFromState(t *testing.T) {
	tests := []struct {
		name              string
		stateFileBaseName string
		want              []Service
	}{
		{
			name:              "no-version",
			stateFileBaseName: "no-version",
			want: []Service{
				{
					Name:          "redis",
					Instance:      "redis",
					ContainerID:   "399366e861976b77e5574c6b956f70dd2473944d822196e8bd6735da7e1d373f",
					ContainerName: "redis",
					IPAddress:     "172.17.0.2",
					ListenAddresses: []facts.ListenAddress{
						{NetworkFamily: "tcp", Address: "172.17.0.2", Port: 6379},
					},
					ExePath:     "/usr/local/bin/redis-server",
					ServiceType: RedisService,
				},
			},
		},
		{
			name:              "no-version-port-conflict",
			stateFileBaseName: "no-version-port-conflict",
			want: []Service{
				{
					Active:        true,
					Name:          "nginx",
					Instance:      "composetest-phpfpm_and_nginx-1",
					ContainerID:   "231aa25b7994847ea8b672cff7cd1d6a95a301dacece589982fd0de78470d7e3",
					ContainerName: "composetest-phpfpm_and_nginx-1",
					IPAddress:     "172.18.0.7",
					ListenAddresses: []facts.ListenAddress{
						{NetworkFamily: "tcp", Address: "172.18.0.7", Port: 80},
					},
					ServiceType:     NginxService,
					HasNetstatInfo:  true,
					LastNetstatInfo: time.Date(2023, 6, 20, 15, 18, 37, 267699930, time.UTC),
				},
				{
					Name:            "phpfpm",
					Instance:        "composetest-phpfpm_and_nginx-1",
					ContainerID:     "231aa25b7994847ea8b672cff7cd1d6a95a301dacece589982fd0de78470d7e3",
					ContainerName:   "composetest-phpfpm_and_nginx-1",
					IPAddress:       "172.18.0.7",
					ListenAddresses: []facts.ListenAddress{},
					ServiceType:     PHPFPMService,
					Active:          true,
					HasNetstatInfo:  false,
				},
				{
					Active:          true,
					Name:            "nginx",
					Instance:        "conflict-other-port",
					ContainerID:     "741852963",
					ContainerName:   "conflict-other-port",
					IPAddress:       "172.18.0.7",
					ListenAddresses: []facts.ListenAddress{},
					ServiceType:     NginxService,
					HasNetstatInfo:  false,
				},
				{
					Name:            "phpfpm",
					Instance:        "conflict-other-port",
					ContainerID:     "741852963",
					ContainerName:   "conflict-other-port",
					IPAddress:       "172.18.0.7",
					ListenAddresses: []facts.ListenAddress{},
					ServiceType:     PHPFPMService,
					Active:          true,
					HasNetstatInfo:  false,
				},
				{
					Active:        true,
					Name:          "postgresql",
					Instance:      "",
					ContainerID:   "",
					ContainerName: "",
					IPAddress:     "127.0.0.1",
					ListenAddresses: []facts.ListenAddress{
						{NetworkFamily: "tcp", Address: "127.0.0.1", Port: 5432},
						{NetworkFamily: "unix", Address: "/var/run/postgresql/.s.PGSQL.5432", Port: 0},
					},
					ServiceType:     PostgreSQLService,
					HasNetstatInfo:  true,
					LastNetstatInfo: time.Date(2023, 6, 20, 15, 18, 37, 249753388, time.UTC),
				},
				{
					Active:        true,
					Name:          "nginx",
					Instance:      "conflict-inactive",
					ContainerID:   "123456789",
					ContainerName: "conflict-inactive",
					IPAddress:     "172.18.0.9",
					ListenAddresses: []facts.ListenAddress{
						{NetworkFamily: "tcp", Address: "172.18.0.9", Port: 80},
					},
					ServiceType:     NginxService,
					HasNetstatInfo:  true,
					LastNetstatInfo: time.Date(2023, 6, 20, 15, 18, 37, 267699930, time.UTC),
				},
				{
					Active:          false,
					Name:            "phpfpm",
					Instance:        "conflict-inactive",
					ContainerID:     "123456789",
					ContainerName:   "conflict-inactive",
					IPAddress:       "172.18.0.9",
					ListenAddresses: []facts.ListenAddress{},
					ServiceType:     PHPFPMService,
					HasNetstatInfo:  false,
				},
			},
		},
		{
			// This state is likely impossible to produce. fixListenAddressConflict should have remove the
			// listenning address on one service.
			// This state was made-up but allow to test that migration is only applied when migrating from v0 to v1.
			name:              "version1-no-two-migration",
			stateFileBaseName: "version1-no-two-migration",
			want: []Service{
				{
					Active:        true,
					Name:          "nginx",
					Instance:      "",
					ContainerID:   "",
					ContainerName: "",
					IPAddress:     "127.0.0.1",
					ListenAddresses: []facts.ListenAddress{
						{NetworkFamily: "tcp", Address: "127.0.0.1", Port: 80},
					},
					ServiceType:     NginxService,
					HasNetstatInfo:  true,
					LastNetstatInfo: time.Date(2023, 6, 20, 15, 18, 37, 267699930, time.UTC),
				},
				{
					Active:        true,
					Name:          "phpfpm",
					Instance:      "",
					ContainerID:   "",
					ContainerName: "",
					IPAddress:     "127.0.0.1",
					ListenAddresses: []facts.ListenAddress{
						{NetworkFamily: "tcp", Address: "127.0.0.1", Port: 80},
					},
					ServiceType:     PHPFPMService,
					HasNetstatInfo:  true,
					LastNetstatInfo: time.Date(2023, 6, 20, 15, 18, 37, 267699930, time.UTC),
				},
			},
		},
	}
	for _, tt := range tests {
		tt := tt

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			memoryState, err := state.LoadReadOnly(
				fmt.Sprintf("testdata/state-%s.json", tt.stateFileBaseName),
				fmt.Sprintf("testdata/state-%s.cache.json", tt.stateFileBaseName),
			)
			if err != nil {
				t.Fatal(err)
			}

			cmpOptions := []cmp.Option{
				cmpopts.IgnoreUnexported(Service{}),
				cmpopts.EquateEmpty(),
				cmpopts.SortSlices(func(x Service, y Service) bool {
					if x.Name < y.Name {
						return true
					}

					if x.Name == y.Name && x.Instance < y.Instance {
						return true
					}

					return false
				}),
			}

			got := servicesFromState(memoryState)

			if diff := cmp.Diff(tt.want, got, cmpOptions...); diff != "" {
				t.Errorf("servicesFromState() mismatch (-want +got)\n%s", diff)
			}

			// Check that write/re-read yield the same result
			emptyState, err := state.LoadReadOnly("", "")
			if err != nil {
				t.Fatal(err)
			}

			saveState(emptyState, serviceListToMap(got))

			got = servicesFromState(emptyState)

			if diff := cmp.Diff(tt.want, got, cmpOptions...); diff != "" {
				t.Errorf("servicesFromState(saveState()) mismatch (-want +got)\n%s", diff)
			}
		})
	}
}
