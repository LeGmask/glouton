package model

import (
	"errors"
	"glouton/logger"
	"glouton/types"
	"time"

	"github.com/gogo/protobuf/proto"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/storage"
)

var (
	errEmptySamples    = errors.New("samples list is empty")
	errInvalidSample   = errors.New("sample is invalid")
	errUnsupportedType = errors.New("unsupported metric type")
)

func FamiliesToMetricPoints(now time.Time, families []*dto.MetricFamily) []types.MetricPoint {
	samples, err := expfmt.ExtractSamples(
		&expfmt.DecodeOptions{Timestamp: model.TimeFromUnixNano(now.UnixNano())},
		families...,
	)
	if err != nil {
		logger.Printf("Conversion of metrics failed, some metrics may be missing: %v", err)
	}

	result := make([]types.MetricPoint, len(samples))

	for i, sample := range samples {
		labels := make(map[string]string, len(sample.Metric))

		for k, v := range sample.Metric {
			labels[string(k)] = string(v)
		}

		result[i] = types.MetricPoint{
			Labels: labels,
			Point: types.Point{
				Time:  sample.Timestamp.Time(),
				Value: float64(sample.Value),
			},
		}
	}

	return result
}

// SamplesToMetricFamily convert a list of sample to a MetricFamilty of given type.
// The mType could be nil which will use the default of MetricType_UNTYPED.
// All samples must belong to the same family, that is have the same name.
func SamplesToMetricFamily(samples []promql.Sample, mType *dto.MetricType) (*dto.MetricFamily, error) {
	if mType == nil {
		mType = dto.MetricType_UNTYPED.Enum()
	}

	if len(samples) == 0 {
		return nil, errEmptySamples
	}

	mf := &dto.MetricFamily{
		Name:   proto.String(samples[0].Metric.Get(types.LabelName)),
		Type:   mType,
		Help:   proto.String(""),
		Metric: make([]*dto.Metric, 0, len(samples)),
	}

	for _, pt := range samples {
		if len(pt.Metric) == 0 {
			return nil, errInvalidSample
		}

		metric := &dto.Metric{
			Label:       make([]*dto.LabelPair, 0, len(pt.Metric)-1),
			TimestampMs: proto.Int64(pt.T),
		}

		for _, l := range pt.Metric {
			if l.Name == types.LabelName {
				continue
			}

			metric.Label = append(metric.Label, &dto.LabelPair{
				Name:  proto.String(l.Name),
				Value: proto.String(l.Value),
			})
		}

		switch mType.String() {
		case dto.MetricType_COUNTER.Enum().String():
			metric.Counter = &dto.Counter{Value: proto.Float64(pt.V)}
		case dto.MetricType_GAUGE.Enum().String():
			metric.Gauge = &dto.Gauge{Value: proto.Float64(pt.V)}
		case dto.MetricType_UNTYPED.Enum().String():
			metric.Untyped = &dto.Untyped{Value: proto.Float64(pt.V)}
		default:
			return nil, errUnsupportedType
		}

		mf.Metric = append(mf.Metric, metric)
	}

	return mf, nil
}

// SendPointsToAppender append all points to given appender. It will mutate points's labels
// to include some meta-labels known by Registry for some annotation.
// This method will not Commit or Rollback on the Appender.
func SendPointsToAppender(points []types.MetricPoint, app storage.Appender) error {
	for _, pts := range points {
		if pts.Annotations.Status.CurrentStatus.IsSet() {
			pts.Labels[types.LabelMetaCurrentStatus] = pts.Annotations.Status.CurrentStatus.String()
			pts.Labels[types.LabelMetaCurrentDescription] = pts.Annotations.Status.StatusDescription
		}

		_, err := app.Append(0, labels.FromMap(pts.Labels), pts.Time.UnixMilli(), pts.Value)
		if err != nil {
			return err
		}
	}

	return nil
}
