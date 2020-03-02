// Copyright 2015-2019 Bleemeo
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

// Package registry package implement a dynamic collection of metrics sources
//
// It support both pushed metrics (using AddMetricPointFunction) and pulled
// metrics thought Collector or Gatherer
//nolint: scopelint
package registry

import (
	"context"
	"glouton/types"
	"reflect"
	"testing"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/pkg/relabel"
)

type fakeCollector struct {
	name      string
	callCount int
}

func (c *fakeCollector) Collect(ch chan<- prometheus.Metric) {
	c.callCount++

	ch <- prometheus.MustNewConstMetric(
		prometheus.NewDesc(c.name, "fake metric", nil, nil),
		prometheus.GaugeValue,
		1.0,
	)
}
func (c *fakeCollector) Describe(chan<- *prometheus.Desc) {

}

type fakeGatherer struct {
	name      string
	callCount int
	response  []*dto.MetricFamily
}

func (g *fakeGatherer) Gather() ([]*dto.MetricFamily, error) {
	g.callCount++

	result := make([]*dto.MetricFamily, len(g.response))

	for i, mf := range g.response {
		b, err := proto.Marshal(mf)
		if err != nil {
			panic(err)
		}
		var tmp dto.MetricFamily
		err = proto.Unmarshal(b, &tmp)
		if err != nil {
			panic(err)
		}
		result[i] = &tmp
	}

	return result, nil
}

func TestRegistry_Register(t *testing.T) {
	reg := &Registry{}

	coll1 := &fakeCollector{
		name: "coll1",
	}
	coll2 := &fakeCollector{
		name: "coll2",
	}
	gather1 := &fakeGatherer{
		name: "gather1",
	}

	if err := reg.Register(coll1); err != nil {
		t.Errorf("reg.Register(coll1) failed: %v", err)
	}
	if err := reg.Register(coll1); err == nil {
		t.Errorf("Second reg.Register(coll1) succeeded, want fail")
	}

	_, _ = reg.Gather()
	if coll1.callCount != 1 {
		t.Errorf("coll1.callCount = %v, want 1", coll1.callCount)
	}

	if !reg.Unregister(coll1) {
		t.Errorf("reg.Unregister(coll1) failed")
	}

	_, _ = reg.Gather()
	if coll1.callCount != 1 {
		t.Errorf("coll1.callCount = %v, want 1", coll1.callCount)
	}

	if err := reg.RegisterWithLabels(coll1, map[string]string{"name": "value"}); err != nil {
		t.Errorf("re-reg.Register(coll1) failed: %v", err)
	}
	if err := reg.Register(coll2); err != nil {
		t.Errorf("re-reg.Register(coll2) failed: %v", err)
	}

	_, _ = reg.Gather()
	if coll1.callCount != 2 {
		t.Errorf("coll1.callCount = %v, want 2", coll1.callCount)
	}
	if coll2.callCount != 1 {
		t.Errorf("coll2.callCount = %v, want 1", coll2.callCount)
	}

	if !reg.Unregister(coll1) {
		t.Errorf("reg.Unregister(coll1) failed")
	}
	if !reg.Unregister(coll2) {
		t.Errorf("reg.Unregister(coll2) failed")
	}

	_, _ = reg.Gather()
	if coll1.callCount != 2 {
		t.Errorf("coll1.callCount = %v, want 2", coll1.callCount)
	}
	if coll2.callCount != 1 {
		t.Errorf("coll2.callCount = %v, want 1", coll2.callCount)
	}

	if err := reg.RegisterGatherer(gather1, map[string]string{"name": "value"}); err != nil {
		t.Errorf("reg.RegisterGatherer(gather1) failed: %v", err)
	}

	_, _ = reg.Gather()
	if coll1.callCount != 2 {
		t.Errorf("coll1.callCount = %v, want 2", coll1.callCount)
	}
	if coll2.callCount != 1 {
		t.Errorf("coll2.callCount = %v, want 1", coll2.callCount)
	}
	if gather1.callCount != 1 {
		t.Errorf("gather1.callCount = %v, want 1", gather1.callCount)
	}

	if !reg.UnregisterGatherer(gather1) {
		t.Errorf("reg.Unregister(coll1) failed")
	}

	_, _ = reg.Gather()
	if coll1.callCount != 2 {
		t.Errorf("coll1.callCount = %v, want 2", coll1.callCount)
	}
	if coll2.callCount != 1 {
		t.Errorf("coll2.callCount = %v, want 1", coll2.callCount)
	}
	if gather1.callCount != 1 {
		t.Errorf("gather1.callCount = %v, want 1", gather1.callCount)
	}

	if err := reg.RegisterWithLabels(coll1, map[string]string{"dummy": "value"}); err != nil {
		t.Errorf("re-reg.Register(coll1) failed: %v", err)
	}
	if err := reg.Register(coll2); err != nil {
		t.Errorf("re-reg.Register(coll2) failed: %v", err)
	}
	reg.UpdateBleemeoAgentID(context.Background(), "fake-uuid")

	result, err := reg.Gather()
	if err != nil {
		t.Error(err)
	}
	helpText := "fake metric"
	jobName := "job"
	jobValue := "glouton"
	dummyName := "dummy"
	dummyValue := "value"
	instanceIDName := "instance_uuid"
	instanceIDValue := "fake-uuid"
	value := 1.0
	want := []*dto.MetricFamily{
		{
			Name: &coll1.name,
			Help: &helpText,
			Type: dto.MetricType_GAUGE.Enum(),
			Metric: []*dto.Metric{
				{
					Label: []*dto.LabelPair{
						{Name: &dummyName, Value: &dummyValue},
						{Name: &instanceIDName, Value: &instanceIDValue},
						{Name: &jobName, Value: &jobValue},
					},
					Gauge: &dto.Gauge{
						Value: &value,
					},
				},
			},
		},
		{
			Name: &coll2.name,
			Help: &helpText,
			Type: dto.MetricType_GAUGE.Enum(),
			Metric: []*dto.Metric{
				{
					Label: []*dto.LabelPair{
						{Name: &instanceIDName, Value: &instanceIDValue},
						{Name: &jobName, Value: &jobValue},
					},
					Gauge: &dto.Gauge{
						Value: &value,
					},
				},
			},
		},
	}

	if !reflect.DeepEqual(result, want) {
		t.Errorf("reg.Gather() = %v, want %v", result, want)
	}
}

func TestRegistry_pushPoint(t *testing.T) {
	reg := &Registry{}

	t0 := time.Date(2020, 3, 2, 10, 30, 0, 0, time.UTC)
	t0MS := t0.UnixNano() / 1e6

	pusher := reg.WithTTL(24 * time.Hour)
	pusher.PushPoints(
		[]types.MetricPoint{
			{
				Point: types.Point{Value: 1.0, Time: t0},
				Labels: map[string]string{
					"__name__": "point1",
					"dummy":    "value",
				},
			},
		},
	)

	got, err := reg.Gather()
	if err != nil {
		t.Error(err)
	}
	metricName := "point1"
	helpText := ""
	jobName := "job"
	jobValue := "glouton"
	dummyName := "dummy"
	dummyValue := "value"
	instanceIDName := "instance_uuid"
	instanceIDValue := "fake-uuid"
	value := 1.0
	want := []*dto.MetricFamily{
		{
			Name: &metricName,
			Help: &helpText,
			Type: dto.MetricType_UNTYPED.Enum(),
			Metric: []*dto.Metric{
				{
					Label: []*dto.LabelPair{
						{Name: &dummyName, Value: &dummyValue},
						{Name: &jobName, Value: &jobValue},
					},
					Untyped: &dto.Untyped{
						Value: &value,
					},
					TimestampMs: &t0MS,
				},
			},
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("reg.Gather() = %v, want %v", got, want)
	}

	reg.UpdateBleemeoAgentID(context.Background(), "fake-uuid")

	got, err = reg.Gather()
	if err != nil {
		t.Error(err)
	}
	if len(got) > 0 {
		t.Errorf("reg.Gather() len=%v, want 0", len(got))
	}

	pusher.PushPoints(
		[]types.MetricPoint{
			{
				Point: types.Point{Value: 1.0, Time: t0},
				Labels: map[string]string{
					"__name__": "point1",
					"dummy":    "value",
				},
			},
		},
	)

	got, err = reg.Gather()
	if err != nil {
		t.Error(err)
	}

	want = []*dto.MetricFamily{
		{
			Name: &metricName,
			Help: &helpText,
			Type: dto.MetricType_UNTYPED.Enum(),
			Metric: []*dto.Metric{
				{
					Label: []*dto.LabelPair{
						{Name: &dummyName, Value: &dummyValue},
						{Name: &instanceIDName, Value: &instanceIDValue},
						{Name: &jobName, Value: &jobValue},
					},
					Untyped: &dto.Untyped{
						Value: &value,
					},
					TimestampMs: &t0MS,
				},
			},
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("reg.Gather() = %v, want %v", got, want)
	}
}

func TestRegistry_applyRelabel(t *testing.T) {

	type fields struct {
		relabelConfigs []*relabel.Config
	}
	type args struct {
		input map[string]string
	}
	tests := []struct {
		name            string
		fields          fields
		args            args
		want            labels.Labels
		wantAnnotations types.MetricAnnotations
	}{
		{
			name:   "node_exporter",
			fields: fields{relabelConfigs: getDefaultRelabelConfig()},
			args: args{map[string]string{
				types.LabelGloutonFQDN: "hostname",
				types.LabelGloutonPort: "8015",
				types.LabelPort:        "8015",
			}},
			want: labels.FromMap(map[string]string{
				"instance": "hostname:8015",
				"job":      "glouton",
			}),
			wantAnnotations: types.MetricAnnotations{},
		},
		{
			name:   "mysql container",
			fields: fields{relabelConfigs: getDefaultRelabelConfig()},
			args: args{map[string]string{
				types.LabelServiceName:   "mysql",
				types.LabelContainerName: "mysql_1",
				types.LabelContainerID:   "1234",
				types.LabelGloutonFQDN:   "hostname",
				types.LabelGloutonPort:   "8015",
				types.LabelServicePort:   "3306",
				types.LabelPort:          "3306",
			}},
			want: labels.FromMap(map[string]string{
				"container_name": "mysql_1",
				"instance":       "hostname-mysql_1:3306",
				"job":            "glouton",
			}),
			wantAnnotations: types.MetricAnnotations{
				ServiceName: "mysql",
				ContainerID: "1234",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Registry{}
			r.relabelConfigs = tt.fields.relabelConfigs
			promLabels, annotations := r.applyRelabel(tt.args.input)
			if !reflect.DeepEqual(promLabels, tt.want) {
				t.Errorf("Registry.applyRelabel() promLabels = %v, want %v", promLabels, tt.want)
			}
			if !reflect.DeepEqual(annotations, tt.wantAnnotations) {
				t.Errorf("Registry.applyRelabel() annotations = %v, want %v", annotations, tt.wantAnnotations)
			}
		})
	}
}
