package check

import (
	"context"
	"glouton/prometheus/model"
	"glouton/types"
	"time"

	dto "github.com/prometheus/client_model/go"
)

const gatherTimeout = 10 * time.Second

// CheckGatherer is the gatherer used for service checks.
type CheckGatherer struct {
	check          Check
	scheduleUpdate func(runAt time.Time)
}

// Check is an interface which specifies a check.
type Check interface {
	Check(ctx context.Context, scheduleUpdate func(runAt time.Time)) types.MetricPoint
}

// NewCheckGatherer returns a new check gatherer.
func NewCheckGatherer(check Check) *CheckGatherer {
	return &CheckGatherer{check: check}
}

// Gather runs the check and returns the result as metric families.
func (cg *CheckGatherer) Gather() ([]*dto.MetricFamily, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gatherTimeout)
	defer cancel()

	point := cg.check.Check(ctx, cg.scheduleUpdate)
	mfs := model.MetricPointsToFamilies([]types.MetricPoint{point})

	return mfs, nil
}

// SetScheduleUpdate implements GathererWithScheduleUpdate.
func (cg *CheckGatherer) SetScheduleUpdate(scheduleUpdate func(runAt time.Time)) {
	cg.scheduleUpdate = scheduleUpdate
}

// CheckNow runs the check and returns its status.
func (cg *CheckGatherer) CheckNow(ctx context.Context) types.StatusDescription {
	ctx, cancel := context.WithTimeout(context.Background(), gatherTimeout)
	defer cancel()

	point := cg.check.Check(ctx, cg.scheduleUpdate)

	return point.Annotations.Status
}
