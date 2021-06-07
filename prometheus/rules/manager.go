// Copyright 2015-2021 Bleemeo
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

package rules

import (
	"context"
	"fmt"
	"glouton/logger"
	"glouton/store"
	"glouton/threshold"
	"glouton/types"
	"math"
	"os"
	"runtime"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/prometheus/prometheus/rules"
)

// Manager is a wrapper handling everything related to prometheus recording
// and alerting rules.
type Manager struct {
	// store implements both appendable and queryable.
	store          *store.Store
	recordingRules []*rules.Group
	alertingRules  map[string]*rules.AlertingRule

	engine *promql.Engine
	logger log.Logger
}

//nolint: gochecknoglobals
var (
	defaultLinuxRecordingRules = map[string]string{
		"node_cpu_seconds_global": "sum(node_cpu_seconds_total) without (cpu)",
	}
	defaultWindowsRecordingRules = map[string]string{
		"windows_cpu_time_global":            "sum(windows_cpu_time_total) without(core)",
		"windows_memory_standby_cache_bytes": "windows_memory_standby_cache_core_bytes+windows_memory_standby_cache_normal_priority_bytes+windows_memory_standby_cache_reserve_bytes",
	}
)

func NewManager(ctx context.Context, store *store.Store) *Manager {
	promLogger := log.NewLogfmtLogger(log.NewSyncWriter(os.Stderr))
	engine := promql.NewEngine(promql.EngineOpts{
		Logger:             log.With(promLogger, "component", "query engine"),
		Reg:                nil,
		MaxSamples:         50000000,
		Timeout:            2 * time.Minute,
		ActiveQueryTracker: nil,
		LookbackDelta:      5 * time.Minute,
	})

	mgrOptions := &rules.ManagerOptions{
		Context:    ctx,
		Logger:     log.With(promLogger, "component", "rules manager"),
		Appendable: store,
		Queryable:  store,
		QueryFunc:  rules.EngineQueryFunc(engine, store),
		NotifyFunc: func(ctx context.Context, expr string, alerts ...*rules.Alert) {
			if len(alerts) == 0 {
				return
			}

			logger.V(2).Printf("notification triggered for expression %x with state %v and labels %v", expr, alerts[0].State, alerts[0].Labels)
		},
	}

	defaultGroupRules := []rules.Rule{}

	defaultRules := defaultLinuxRecordingRules
	if runtime.GOOS == "windows" {
		defaultRules = defaultWindowsRecordingRules
	}

	for metricName, val := range defaultRules {
		exp, err := parser.ParseExpr(val)
		if err != nil {
			logger.V(0).Printf("An error occurred while parsing expression %s: %v. This rule was not registered", val, err)
		} else {
			newRule := rules.NewRecordingRule(metricName, exp, labels.Labels{})
			defaultGroupRules = append(defaultGroupRules, newRule)
		}
	}

	defaultGroup := rules.NewGroup(rules.GroupOptions{
		Name:          "default",
		Rules:         defaultGroupRules,
		ShouldRestore: true,
		Opts:          mgrOptions,
	})

	rm := Manager{
		store:          store,
		recordingRules: []*rules.Group{defaultGroup},
		alertingRules:  make(map[string]*rules.AlertingRule),
		engine:         engine,
		logger:         promLogger,
	}

	return &rm
}

func (rm *Manager) Run() {
	ctx := context.Background()
	now := time.Now()
	points := []types.MetricPoint{}

	for _, rgr := range rm.recordingRules {
		rgr.Eval(ctx, now)
	}

	for _, agr := range rm.alertingRules {
		prevState := agr.State()

		queryable := &store.CountingQueryable{Queryable: rm.store}
		_, err := agr.Eval(ctx, now, rules.EngineQueryFunc(rm.engine, queryable), nil)

		if err != nil {
			logger.V(2).Printf("an error occurred while evaluating an alerting rule: %w", err)
			continue
		}

		if queryable.Count() == 0 {
			continue
		}

		state := agr.State()

		if state != prevState {
			logger.V(2).Printf("metric state for %s changed: previous state=%v, new state=%v", agr.Name(), prevState, state)
			nagiosCode := types.NagiosCodeFromString(agr.Labels().Get("severity"))

			status := types.StatusDescription{
				CurrentStatus:     types.FromNagios(nagiosCode),
				StatusDescription: "",
			}

			newPoint := types.MetricPoint{
				Point: types.Point{
					Time:  now,
					Value: float64(nagiosCode),
				},
				Annotations: types.MetricAnnotations{
					Status: status,
				},
			}

			if state == rules.StateFiring || prevState == rules.StateFiring {
				if state != rules.StateFiring && prevState == rules.StateFiring {
					newPoint.Value = float64(types.NagiosCodeFromString("ok"))
				}

				points = append(points, newPoint)
			}
		}
	}

	if len(points) != 0 {
		rm.store.PushPoints(points)
	}
}

func (rm *Manager) newRule(exp string, metricName string, threshold string, hold time.Duration, severity string) error {
	newExp, err := parser.ParseExpr(exp)
	if err != nil {
		return err
	}

	newRule := rules.NewAlertingRule(metricName+threshold,
		newExp, hold, labels.Labels{labels.Label{Name: "severity", Value: severity}},
		labels.Labels{labels.Label{Name: types.LabelName, Value: metricName}}, labels.Labels{}, false, log.With(rm.logger, "alerting_rule", metricName+threshold))

	rm.alertingRules[metricName+threshold] = newRule

	return nil
}

func (rm *Manager) addAlertingRule(name string, exp string, hold time.Duration, thresholds threshold.Threshold) error {
	if len(exp) == 0 {
		return nil
	}

	if !math.IsNaN(thresholds.LowWarning) {
		err := rm.newRule(exp+fmt.Sprintf("> %f", thresholds.LowWarning), name, "_low_warning", hold, "warning")
		if err != nil {
			return err
		}
	}

	if !math.IsNaN(thresholds.HighWarning) {
		err := rm.newRule(exp+fmt.Sprintf("> %f", thresholds.HighWarning), name, "_high_warning", hold, "warning")
		if err != nil {
			return err
		}
	}

	if !math.IsNaN(thresholds.LowCritical) {
		err := rm.newRule(exp+fmt.Sprintf("> %f", thresholds.LowCritical), name, "_low_critical", hold, "critical")
		if err != nil {
			return err
		}
	}

	if !math.IsNaN(thresholds.HighCritical) {
		err := rm.newRule(exp+fmt.Sprintf("> %f", thresholds.HighCritical), name, "_high_critical", hold, "critical")
		if err != nil {
			return err
		}
	}

	return nil
}

func (rm *Manager) deleteAlertingRule(name string) {
	delete(rm.alertingRules, name)
}

//UpdateAlertingRule updates an alerting rule set.
// It will either create the rule set ([low|high][warning|critical]) or replace the old one.
func (rm *Manager) UpdateAlertingRule(name string, exp string, hold time.Duration, thresholds threshold.Threshold) error {
	rm.deleteAlertingRule(name)
	return rm.addAlertingRule(name, exp, hold, thresholds)
}
