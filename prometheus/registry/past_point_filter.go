package registry

import (
	"context"
	"sync"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/model"
)

type GathererWithOrWithoutState interface {
	Gather() ([]*dto.MetricFamily, error)
	GatherWithState(ctx context.Context, state GatherState) ([]*dto.MetricFamily, error)
}

type point struct {
	timestampMs int64
	recordedAt  time.Time
}

type pastPointFilter struct {
	gatherer GathererWithOrWithoutState

	latestPointByLabelsByMetric map[string]map[uint64]point

	purgeInterval time.Duration
	lastPurgeAt   time.Time

	timeNow func() time.Time // For testing purposes

	l sync.Mutex
}

func WithPastPointFilter(gatherer GathererWithOrWithoutState, purgeInterval time.Duration) GathererWithOrWithoutState {
	return &pastPointFilter{
		gatherer:                    gatherer,
		latestPointByLabelsByMetric: make(map[string]map[uint64]point),
		purgeInterval:               purgeInterval,
		lastPurgeAt:                 time.Now(),
		timeNow:                     time.Now,
	}
}

func (ppf *pastPointFilter) Gather() ([]*dto.MetricFamily, error) {
	return ppf.filter(ppf.gatherer.Gather())
}

func (ppf *pastPointFilter) GatherWithState(ctx context.Context, state GatherState) ([]*dto.MetricFamily, error) {
	return ppf.filter(ppf.gatherer.GatherWithState(ctx, state))
}

func (ppf *pastPointFilter) filter(mfs []*dto.MetricFamily, err error) ([]*dto.MetricFamily, error) {
	if err != nil {
		return mfs, err
	}

	ppf.l.Lock()
	defer ppf.l.Unlock()

	now := ppf.timeNow().Truncate(time.Second)

	for _, mf := range mfs {
		if mf == nil {
			continue
		}

		latestPointByLabelSignatures, found := ppf.latestPointByLabelsByMetric[mf.GetName()]
		if !found {
			latestPointByLabelSignatures = make(map[uint64]point, len(mf.GetMetric()))
		}

		m := 0

		for i := 0; i < len(mf.Metric); i++ { //nolint:protogetter
			metric := mf.Metric[i] //nolint:protogetter
			if metric == nil {
				continue
			}

			signature := labelPairsToSignature(metric.GetLabel())
			currentTimestamp := metric.GetTimestampMs()

			if latestPoint, found := latestPointByLabelSignatures[signature]; found {
				if latestPoint.timestampMs > currentTimestamp {
					continue // This metric jumped backward, drop it.
				}
			}

			latestPointByLabelSignatures[signature] = point{currentTimestamp, now}
			mf.Metric[m] = metric
			m++
		}

		mf.Metric = mf.Metric[:m] //nolint:protogetter
		ppf.latestPointByLabelsByMetric[mf.GetName()] = latestPointByLabelSignatures
	}

	go ppf.runPurge(now)

	return mfs, nil
}

func (ppf *pastPointFilter) runPurge(now time.Time) {
	ppf.l.Lock()
	defer ppf.l.Unlock()

	if now.Sub(ppf.lastPurgeAt) < ppf.purgeInterval {
		return
	}

	for metric, latestPointByLabelSignatures := range ppf.latestPointByLabelsByMetric {
		for signature, point := range latestPointByLabelSignatures {
			if now.Sub(point.recordedAt) > ppf.purgeInterval {
				delete(latestPointByLabelSignatures, signature)
			}
		}

		if len(latestPointByLabelSignatures) == 0 {
			delete(ppf.latestPointByLabelsByMetric, metric)
		}
	}

	ppf.lastPurgeAt = now
}

func labelPairsToSignature(labelPairs []*dto.LabelPair) uint64 {
	labels := make(map[string]string, len(labelPairs))

	for _, labelPair := range labelPairs {
		labels[labelPair.GetName()] = labelPair.GetValue()
	}

	return model.LabelsToSignature(labels)
}
