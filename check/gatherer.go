// Copyright 2015-2022 Bleemeo
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

package check

import (
	"context"
	"glouton/logger"
	"glouton/prometheus/model"
	"glouton/prometheus/registry"
	"glouton/types"
	"sync"
	"time"

	dto "github.com/prometheus/client_model/go"
)

const defaultGatherTimeout = 10 * time.Second

// Gatherer is the gatherer used for service checks.
type Gatherer struct {
	check          checker
	scheduleUpdate func(runAt time.Time)

	l sync.Mutex
	// The metrics produced by the check are kept to be returned when
	// the gatherer is called from /metrics.
	lastMetricFamilies []*dto.MetricFamily
}

// checker is an interface which specifies a check.
type checker interface {
	Check(ctx context.Context, scheduleUpdate func(runAt time.Time)) types.MetricPoint
	Close()
}

// NewCheckGatherer returns a new check gatherer.
func NewCheckGatherer(check checker) *Gatherer {
	return &Gatherer{check: check}
}

// GatherWithState implements GathererWithState.
func (cg *Gatherer) GatherWithState(ctx context.Context, state registry.GatherState) ([]*dto.MetricFamily, error) {
	// Return the metrics from the last check on /metrics.
	if !state.FromScrapeLoop {
		cg.l.Lock()
		mfs := cg.lastMetricFamilies
		cg.l.Unlock()

		return mfs, nil
	}

	point := cg.check.Check(ctx, cg.scheduleUpdate)
	mfs := model.MetricPointsToFamilies([]types.MetricPoint{point})

	cg.l.Lock()
	cg.lastMetricFamilies = mfs
	cg.l.Unlock()

	return mfs, nil
}

// Gather runs the check and returns the result as metric families.
func (cg *Gatherer) Gather() ([]*dto.MetricFamily, error) {
	logger.V(2).Println("Gather() called directly on a check gatherer, this is a bug!")

	ctx, cancel := context.WithTimeout(context.Background(), defaultGatherTimeout)
	defer cancel()

	return cg.GatherWithState(ctx, registry.GatherState{})
}

// SetScheduleUpdate implements GathererWithScheduleUpdate.
func (cg *Gatherer) SetScheduleUpdate(scheduleUpdate func(runAt time.Time)) {
	cg.scheduleUpdate = scheduleUpdate
}

// CheckNow runs the check and returns its status.
func (cg *Gatherer) CheckNow(ctx context.Context) types.StatusDescription {
	point := cg.check.Check(ctx, cg.scheduleUpdate)

	return point.Annotations.Status
}

func (cg *Gatherer) Close() {
	cg.check.Close()
}
