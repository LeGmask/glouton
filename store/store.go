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

// Package store implement a Metric/MetricPoint store.
//
// currently the storage in only in-memory and not persisted.
package store

import (
	"context"
	"errors"
	"fmt"
	"glouton/logger"
	"glouton/types"
	"math"
	"reflect"
	"sort"
	"sync"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/value"
)

var errDeletedMetric = errors.New("metric was deleted")

// Store implement an interface to retrieve metrics and metric points.
//
// See methods GetMetrics and GetMetricPoints.
type Store struct {
	metrics           map[uint64]metric
	points            map[uint64][]types.Point
	notifyCallbacks   map[int]func([]types.MetricPoint)
	newMetricCallback func([]types.LabelsAndAnnotation)
	maxPointsAge      time.Duration
	maxMetricsAge     time.Duration
	workLabels        labels.Labels
	lock              sync.Mutex
	notifeeLock       sync.Mutex
	resetRuleLock     sync.Mutex
	nowFunc           func() time.Time
}

// New create a return a store. Store should be Close()d before leaving.
func New(maxPointsAge time.Duration, maxMetricsAge time.Duration) *Store {
	s := &Store{
		metrics:         make(map[uint64]metric),
		points:          make(map[uint64][]types.Point),
		notifyCallbacks: make(map[int]func([]types.MetricPoint)),
		maxPointsAge:    maxPointsAge,
		maxMetricsAge:   maxMetricsAge,
		nowFunc:         time.Now,
	}

	return s
}

func (s *Store) DiagnosticArchive(ctx context.Context, archive types.ArchiveWriter) error {
	file, err := archive.Create("store.txt")
	if err != nil {
		return err
	}

	s.lock.Lock()

	var (
		oldestTime   time.Time
		youngestTime time.Time
		pointsCount  int
	)

	for _, pts := range s.points {
		pointsCount += len(pts)

		for _, p := range pts {
			if oldestTime.IsZero() || p.Time.Before(oldestTime) {
				oldestTime = p.Time
			}

			if youngestTime.IsZero() || p.Time.After(youngestTime) {
				youngestTime = p.Time
			}
		}
	}

	metricsCount := len(s.metrics)

	s.lock.Unlock()

	fmt.Fprintln(file, "Metric store:")
	fmt.Fprintf(file, "metrics count: %d\n", metricsCount)
	fmt.Fprintf(file, "points count: %d\n", pointsCount)
	fmt.Fprintf(file, "points time range: %v to %v\n", oldestTime, youngestTime)

	return nil
}

// Run will run the store until context is cancelled.
func (s *Store) Run(ctx context.Context) error {
	for {
		s.RunOnce()

		select {
		case <-time.After(300 * time.Second):
		case <-ctx.Done():
			return nil
		}
	}
}

// RunOnce runs the store once to remove old points and metrics.
func (s *Store) RunOnce() {
	s.run(s.nowFunc())
}

// AddNotifiee add a callback that will be notified of all points received
// Note: AddNotifiee should not be called while in the callback.
func (s *Store) AddNotifiee(cb func([]types.MetricPoint)) int {
	s.notifeeLock.Lock()
	defer s.notifeeLock.Unlock()

	id := 1
	_, ok := s.notifyCallbacks[id]

	for ok {
		id++
		if id == 0 {
			panic("too many notifiee in the store. Unable to find new slot")
		}

		_, ok = s.notifyCallbacks[id]
	}

	s.notifyCallbacks[id] = cb

	return id
}

// RemoveNotifiee remove a callback that was notified
// Note: RemoveNotifiee should not be called while in the callback.
// Once RemoveNotifiee() returns, the callbacl won't be called anymore.
func (s *Store) RemoveNotifiee(id int) {
	s.notifeeLock.Lock()
	defer s.notifeeLock.Unlock()

	delete(s.notifyCallbacks, id)
}

// SetNewMetricCallback sets the callback used when a new metrics is seen the first time.
func (s *Store) SetNewMetricCallback(fc func([]types.LabelsAndAnnotation)) {
	s.resetRuleLock.Lock()
	defer s.resetRuleLock.Unlock()

	s.newMetricCallback = fc
}

// DropMetrics delete metrics and they points.
// The provided labels list is an exact match (e.g. {"__name__": "disk_used"} won't delete the metrics for all disk. You need to specify all labels).
func (s *Store) DropMetrics(labelsList []map[string]string) {
	s.lock.Lock()
	defer s.lock.Unlock()

	for i, m := range s.metrics {
		for _, l := range labelsList {
			if reflect.DeepEqual(m.labels, l) {
				delete(s.metrics, i)
				delete(s.points, i)
			}
		}
	}
}

// DropAllMetrics clear the full content of the store.
func (s *Store) DropAllMetrics() {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.metrics = make(map[uint64]metric)
	s.points = make(map[uint64][]types.Point)
}

// Metrics return a list of Metric matching given labels filter.
func (s *Store) Metrics(filters map[string]string) (result []types.Metric, err error) {
	result = make([]types.Metric, 0)

	s.lock.Lock()
	defer s.lock.Unlock()

	for _, m := range s.metrics {
		if labelsMatch(m.labels, filters, false) {
			result = append(result, m)
		}
	}

	return
}

// MetricsCount return the count of metrics stored.
func (s *Store) MetricsCount() int {
	s.lock.Lock()
	defer s.lock.Unlock()

	return len(s.metrics)
}

// Labels returns all label of the metric.
func (m metric) Labels() map[string]string {
	return m.labels
}

// Annotations returns all annotations of the metric.
func (m metric) Annotations() types.MetricAnnotations {
	return m.annotations
}

// Points returns points between the two given time range (boundary are included).
func (m metric) Points(start, end time.Time) (result []types.Point, err error) {
	m.store.lock.Lock()
	defer m.store.lock.Unlock()

	if !m.store.metrics[m.metricID].createAt.Equal(m.createAt) {
		return nil, errDeletedMetric
	}

	points := m.store.points[m.metricID]
	result = make([]types.Point, 0)

	for _, point := range points {
		pointTimeUTC := point.Time.UTC()
		if !pointTimeUTC.Before(start) && !pointTimeUTC.After(end) {
			result = append(result, point)
		}
	}

	return
}

// LastPointReceivedAt return the last time a point was received.
func (m metric) LastPointReceivedAt() time.Time {
	return m.lastPoint
}

type metric struct {
	labels      map[string]string
	annotations types.MetricAnnotations
	store       *Store
	metricID    uint64
	createAt    time.Time
	lastPoint   time.Time
}

// Return true if filter match given labels.
func labelsMatch(labels, filter map[string]string, exact bool) bool {
	if exact && len(labels) != len(filter) {
		return false
	}

	for k, v := range filter {
		if v2, ok := labels[k]; !ok || v2 != v {
			return false
		}
	}

	return true
}

func (s *Store) run(now time.Time) {
	s.lock.Lock()
	defer s.lock.Unlock()

	deletedPoints := 0
	totalPoints := 0
	metricToDelete := make([]uint64, 0)

	for metricID, metric := range s.metrics {
		points := s.points[metricID]
		newPoints := make([]types.Point, 0)

		for _, p := range points {
			if now.Sub(p.Time) < s.maxPointsAge {
				newPoints = append(newPoints, p)
			}
		}

		if len(newPoints) == 0 && now.Sub(metric.lastPoint) >= s.maxMetricsAge {
			metricToDelete = append(metricToDelete, metricID)
		} else {
			s.points[metricID] = newPoints
		}

		totalPoints += len(newPoints)
		deletedPoints += len(points) - len(newPoints)
	}

	for _, metricID := range metricToDelete {
		delete(s.metrics, metricID)
		delete(s.points, metricID)
	}

	logger.V(2).Printf("deleted %d points. Total point: %d", deletedPoints, totalPoints)
}

// metricGet will return the metric that exactly match given labels.
//
// If won't create the metric if it does not exists but it return the metric ready to be added to s.metrics.
// The store lock is assumed to be held.
// Annotations is always updated with value provided as argument if the metric exists.
func (s *Store) metricGet(lbls map[string]string, annotations types.MetricAnnotations) (metric, bool) {
	if cap(s.workLabels) < len(lbls) {
		s.workLabels = make(labels.Labels, len(lbls))
	}

	s.workLabels = s.workLabels[:0]

	for k, v := range lbls {
		s.workLabels = append(s.workLabels, labels.Label{Name: k, Value: v})
	}

	sort.Sort(s.workLabels)

	hash := s.workLabels.Hash()

	m, ok := s.metrics[hash]
	if ok {
		m.annotations = annotations
		s.metrics[hash] = m

		return m, true
	}

	m = metric{
		labels:      lbls,
		annotations: annotations,
		store:       s,
		metricID:    hash,
		createAt:    s.nowFunc(),
	}

	return m, false
}

// PushPoints append new metric points to the store, creating new metric
// if needed.
// The points must not be mutated after this call.
//
// Writing the value StaleNaN is used to mark the metric as inactive.
func (s *Store) PushPoints(_ context.Context, points []types.MetricPoint) {
	dedupPoints := make([]types.MetricPoint, 0, len(points))

	var newMetrics []types.LabelsAndAnnotation

	s.lock.Lock()
	for _, point := range points {
		metric, found := s.metricGet(point.Labels, point.Annotations)
		length := len(s.points[metric.metricID])

		if length > 0 && s.points[metric.metricID][length-1].Time.Equal(point.Time) {
			continue
		}

		if math.Float64bits(point.Value) == value.StaleNaN {
			// Metric is inactive, delete it
			delete(s.metrics, metric.metricID)
			delete(s.points, metric.metricID)

			continue
		}

		if !found {
			newMetrics = append(newMetrics, types.LabelsAndAnnotation{Labels: point.Labels, Annotations: point.Annotations})
		}

		metric.lastPoint = s.nowFunc()
		s.metrics[metric.metricID] = metric
		s.points[metric.metricID] = append(s.points[metric.metricID], point.Point)
		dedupPoints = append(dedupPoints, point)
	}

	s.lock.Unlock()
	s.resetRuleLock.Lock()

	cb := s.newMetricCallback

	s.resetRuleLock.Unlock()

	if len(newMetrics) > 0 && cb != nil {
		cb(newMetrics)
	}

	s.notifeeLock.Lock()

	for _, cb := range s.notifyCallbacks {
		cb(dedupPoints)
	}

	s.notifeeLock.Unlock()
}

// InternalSetNowAndRunOnce is used for testing.
// It will set the Now() function used by the store and will call one loop of Run() method
// which does purge of older metrics.
func (s *Store) InternalSetNowAndRunOnce(ctx context.Context, nowFunc func() time.Time) {
	s.nowFunc = nowFunc
	s.RunOnce()
}

type store interface {
	Metrics(filters map[string]string) (result []types.Metric, err error)
	MetricsCount() int
	DropMetrics(labelsList []map[string]string)
	AddNotifiee(func([]types.MetricPoint)) int
	RemoveNotifiee(int)
	PushPoints(ctx context.Context, points []types.MetricPoint)
}

// FilteredStore is a store wrapper that intercepts all call to pushPoints and execute filters on points.
type FilteredStore struct {
	store                store
	filterCallback       func([]types.MetricPoint) []types.MetricPoint
	filterMetricCallback func([]types.Metric) []types.Metric
}

// NewFilteredStore initializes a new filtered store.
func NewFilteredStore(store store, fc func([]types.MetricPoint) []types.MetricPoint, fmc func([]types.Metric) []types.Metric) *FilteredStore {
	filteredStore := &FilteredStore{
		store:                store,
		filterCallback:       nil,
		filterMetricCallback: nil,
	}

	filteredStore.filterMetricCallback = fmc
	filteredStore.filterCallback = fc

	return filteredStore
}

// PushPoints wraps the store PushPoints function. It precedes the call with filterCallback.
func (s *FilteredStore) PushPoints(ctx context.Context, points []types.MetricPoint) {
	if s.filterCallback != nil && len(points) > 0 {
		points = s.filterCallback(points)
	}

	s.store.PushPoints(ctx, points)
}

func (s *FilteredStore) Metrics(filters map[string]string) (result []types.Metric, err error) {
	res, err := s.store.Metrics(filters)

	res = s.filterMetricCallback(res)

	return res, err
}

func (s *FilteredStore) MetricsCount() int {
	res, err := s.Metrics(map[string]string{})
	if err != nil {
		logger.V(2).Printf("An error occurred while fetching metrics for filtered store: %w", err)
	}

	return len(res)
}

func (s *FilteredStore) DropMetrics(labelsList []map[string]string) {
	s.store.DropMetrics(labelsList)
}

func (s *FilteredStore) AddNotifiee(fc func([]types.MetricPoint)) int {
	return s.store.AddNotifiee(func(mp []types.MetricPoint) {
		res := s.filterCallback(mp)
		fc(res)
	})
}

func (s *FilteredStore) RemoveNotifiee(v int) {
	s.store.RemoveNotifiee(v)
}
