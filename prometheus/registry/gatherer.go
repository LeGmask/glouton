package registry

import (
	"context"
	"errors"
	"fmt"
	"glouton/logger"
	"glouton/types"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
)

const defaultGatherTimeout = 10 * time.Second

var errIncorrectType = errors.New("incorrect type for gathered metric family")

type queryType int

const (
	// NoProbe is the default value. Probes can be very costly as it involves network calls, hence it is disabled by default.
	NoProbe queryType = iota
	// OnlyProbes specifies we only want data from the probes.
	OnlyProbes
	// All specifies we want all the data, including probes.
	All
)

// GatherState is an argument given to gatherers that support it. It allows us to give extra informations
// to gatherers. Due to the way such objects are contructed when no argument is supplied (when calling
// Gather() on a GathererWithState, most of the time Gather() will directly call GatherWithState(GatherState{}),
// please make sure that default values are sensible. For example, NoProbe *must* be the default queryType, as
// we do not want queries on /metrics to always probe the collectors by default).
type GatherState struct {
	QueryType      queryType
	FromScrapeLoop bool
	T0             time.Time
	NoFilter       bool
}

// GatherStateFromMap creates a GatherState from a state passed as a map.
func GatherStateFromMap(params map[string][]string) GatherState {
	state := GatherState{}

	// TODO: add this in some user-facing documentation
	if _, includeProbes := params["includeMonitors"]; includeProbes {
		state.QueryType = All
	}

	// TODO: add this in some user-facing documentation
	if _, excludeMetrics := params["onlyMonitors"]; excludeMetrics {
		state.QueryType = OnlyProbes
	}

	if _, noFilter := params["noFilter"]; noFilter {
		state.NoFilter = true
	}

	return state
}

// GathererWithState is a generalization of prometheus.Gather.
type GathererWithState interface {
	GatherWithState(context.Context, GatherState) ([]*dto.MetricFamily, error)
}

// GathererWithScheduleUpdate is a Gatherer that had a ScheduleUpdate (like Probe gatherer).
// The ScheduleUpdate could be used to trigger an additional gather earlier than default scrape interval.
type GathererWithScheduleUpdate interface {
	SetScheduleUpdate(func(runAt time.Time))
}

// GathererWithStateWrapper is a wrapper around GathererWithState that allows to specify a state to forward
// to the wrapped gatherer when the caller does not know about GathererWithState and uses raw Gather().
// The main use case is the /metrics HTTP endpoint, where we want to be able to gather() only some metrics
// (e.g. all metrics/only probes/no probes).
// In the prometheus exporter endpoint, when receiving an request, the (user-provided)
// HTTP handler will:
// - create a new wrapper instance, generate a GatherState accordingly, and call wrapper.setState(newState).
// - pass the wrapper to a new prometheus HTTP handler.
// - when Gather() is called upon the wrapper by prometheus, the wrapper calls GathererWithState(newState)
// on its internal gatherer.
// GatherWithState also contains the metrics allow/deny list in order to sync the metrics on /metric
// with the metrics sent to the bleemeo platform.
type GathererWithStateWrapper struct {
	gatherState GatherState
	gatherer    GathererWithState
	filter      metricFilter
	ctx         context.Context
}

// NewGathererWithStateWrapper creates a new wrapper around GathererWithState.
func NewGathererWithStateWrapper(ctx context.Context, g GathererWithState, filter metricFilter) *GathererWithStateWrapper {
	return &GathererWithStateWrapper{gatherer: g, filter: filter, ctx: ctx}
}

// SetState updates the state the wrapper will provide to its internal gatherer when called.
func (w *GathererWithStateWrapper) SetState(state GatherState) {
	w.gatherState = state
}

// Gather implements prometheus.Gatherer for GathererWithStateWrapper.
func (w *GathererWithStateWrapper) Gather() ([]*dto.MetricFamily, error) {
	res, err := w.gatherer.GatherWithState(w.ctx, w.gatherState)
	if err != nil {
		logger.V(2).Printf("Error during gather on /metrics: %v", err)
	}

	if !w.gatherState.NoFilter {
		res = w.filter.FilterFamilies(res)
	}

	return res, err
}

// labeledGatherer provide a gatherer that will add provided labels to all metrics.
// It also allow to gather to MetricPoints.
type labeledGatherer struct {
	source      prometheus.Gatherer
	labels      []*dto.LabelPair
	annotations types.MetricAnnotations
}

func newLabeledGatherer(g prometheus.Gatherer, extraLabels labels.Labels, annotations types.MetricAnnotations) labeledGatherer {
	labels := make([]*dto.LabelPair, 0, len(extraLabels))

	for _, l := range extraLabels {
		l := l
		if !strings.HasPrefix(l.Name, model.ReservedLabelPrefix) {
			labels = append(labels, &dto.LabelPair{
				Name:  &l.Name,
				Value: &l.Value,
			})
		}
	}

	return labeledGatherer{
		source:      g,
		labels:      labels,
		annotations: annotations,
	}
}

func dtoLabelToMap(lbls []*dto.LabelPair) map[string]string {
	result := make(map[string]string, len(lbls))
	for _, l := range lbls {
		result[l.GetName()] = l.GetValue()
	}

	return result
}

// Gather implements prometheus.Gather.
func (g labeledGatherer) Gather() ([]*dto.MetricFamily, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultGatherTimeout)
	defer cancel()

	return g.GatherWithState(ctx, GatherState{})
}

// GatherWithState implements GathererWithState.
func (g labeledGatherer) GatherWithState(ctx context.Context, state GatherState) ([]*dto.MetricFamily, error) {
	// do not collect non-probes metrics when the user only wants probes
	if _, probe := g.source.(*ProbeGatherer); !probe && state.QueryType == OnlyProbes {
		return nil, nil
	}

	var mfs []*dto.MetricFamily

	var err error

	if cg, ok := g.source.(GathererWithState); ok {
		mfs, err = cg.GatherWithState(ctx, state)
	} else {
		mfs, err = g.source.Gather()
	}

	if len(g.labels) == 0 {
		return mfs, err
	}

	for _, mf := range mfs {
		for i, m := range mf.Metric {
			m.Label = mergeLabels(m.Label, g.labels)
			mf.Metric[i] = m
		}
	}

	return mfs, err
}

// mergeLabels merge two sorted list of labels. In case of name conflict, value from b wins.
func mergeLabels(a []*dto.LabelPair, b []*dto.LabelPair) []*dto.LabelPair {
	result := make([]*dto.LabelPair, 0, len(a)+len(b))
	aIndex := 0

	for _, bLabel := range b {
		for aIndex < len(a) && a[aIndex].GetName() < bLabel.GetName() {
			result = append(result, a[aIndex])
			aIndex++
		}

		if aIndex < len(a) && a[aIndex].GetName() == bLabel.GetName() {
			aIndex++
		}

		result = append(result, bLabel)
	}

	for aIndex < len(a) {
		result = append(result, a[aIndex])
		aIndex++
	}

	return result
}

func (g labeledGatherer) GatherPoints(ctx context.Context, now time.Time, state GatherState) ([]types.MetricPoint, error) {
	mfs, err := g.GatherWithState(ctx, state)
	points := familiesToMetricPoints(now, mfs)

	if (g.annotations != types.MetricAnnotations{}) {
		for i := range points {
			points[i].Annotations = g.annotations
		}
	}

	return points, err
}

type sliceGatherer []*dto.MetricFamily

// Gather implements Gatherer.
func (s sliceGatherer) Gather() ([]*dto.MetricFamily, error) {
	return s, nil
}

// Gatherers do the same as prometheus.Gatherers but allow different gatherer
// to have different metric help text.
// The type must still be the same.
//
// This is useful when scrapping multiple endpoints which provide the same metric
// (like "go_gc_duration_seconds") but changed its help_text.
//
// The first help_text win.
type Gatherers []prometheus.Gatherer

// GatherWithState implements GathererWithState.
func (gs Gatherers) GatherWithState(ctx context.Context, state GatherState) ([]*dto.MetricFamily, error) {
	metricFamiliesByName := map[string]*dto.MetricFamily{}

	var errs prometheus.MultiError

	var mfs []*dto.MetricFamily

	wg := sync.WaitGroup{}
	wg.Add(len(gs))

	mutex := sync.Mutex{}

	// run gather in parallel
	for _, g := range gs {
		go func(g prometheus.Gatherer) {
			defer wg.Done()

			var currentMFs []*dto.MetricFamily

			var err error

			if cg, ok := g.(GathererWithState); ok {
				currentMFs, err = cg.GatherWithState(ctx, state)
			} else {
				currentMFs, err = g.Gather()
			}

			mutex.Lock()

			if err != nil {
				errs = append(errs, err)
			}

			mfs = append(mfs, currentMFs...)

			mutex.Unlock()
		}(g)
	}

	wg.Wait()

	for _, mf := range mfs {
		existingMF, exists := metricFamiliesByName[mf.GetName()]

		if exists {
			if existingMF.GetType() != mf.GetType() {
				errs = append(errs, fmt.Errorf(
					"%w: %s has type %s but should have %s", errIncorrectType,
					mf.GetName(), mf.GetType(), existingMF.GetType(),
				))

				continue
			}
		} else {
			existingMF = &dto.MetricFamily{}
			existingMF.Name = mf.Name
			existingMF.Help = mf.Help
			existingMF.Type = mf.Type
			metricFamiliesByName[mf.GetName()] = existingMF
		}

		existingMF.Metric = append(existingMF.Metric, mf.Metric...)
	}

	result := make([]*dto.MetricFamily, 0, len(metricFamiliesByName))

	for _, f := range metricFamiliesByName {
		result = append(result, f)
	}

	// Still use prometheus.Gatherers because it will:
	// * Remove (and complain) about possible duplicate of metric
	// * sort output
	// * maybe other sanity check :)

	promGatherers := prometheus.Gatherers{
		sliceGatherer(result),
	}

	sortedResult, err := promGatherers.Gather()
	if err != nil {
		if multiErr, ok := err.(prometheus.MultiError); ok {
			errs = append(errs, multiErr...)
		} else {
			errs = append(errs, err)
		}
	}

	return sortedResult, errs.MaybeUnwrap()
}

// Gather implements prometheus.Gather.
func (gs Gatherers) Gather() ([]*dto.MetricFamily, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultGatherTimeout)
	defer cancel()

	return gs.GatherWithState(ctx, GatherState{})
}
