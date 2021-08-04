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
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"glouton/logger"
	"glouton/store"
	"glouton/threshold"
	"glouton/types"
	"os"
	"runtime"
	"sync"
	"time"

	bleemeoTypes "glouton/bleemeo/types"

	"github.com/go-kit/kit/log"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/prometheus/prometheus/rules"
)

// promAlertTime represents the duration for which the alerting rule
// should exceed the threshold to be considered fired.
const promAlertTime = 5 * time.Minute

// minResetTime is the minimum time used by the inactive checks.
// The bleemeo connector uses 15 seconds as a synchronization time,
// thus we add a bit a leeway in the disabled countdown.
const minResetTime = 17 * time.Second

const lowWarningState = "low_warning"
const highWarningState = "high_warning"
const lowCriticalState = "low_critical"
const highCriticalState = "high_critical"

var errUnknownState = errors.New("unknown state for metric")

//Manager is a wrapper handling everything related to prometheus recording
// and alerting rules.
type Manager struct {
	// store implements both appendable and queryable.
	store          *store.Store
	recordingRules []*rules.Group
	alertingRules  map[string]*ruleGroup

	engine *promql.Engine
	logger log.Logger

	l                sync.Mutex
	agentStarted     time.Time
	metricResolution time.Duration
}

type ruleGroup struct {
	rules         map[string]*rules.AlertingRule
	inactiveSince time.Time
	disabledUntil time.Time

	labelsText  string
	promql      string
	thresholds  threshold.Threshold
	isUserAlert bool
	labels      map[string]string
	isError     string
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

func NewManager(ctx context.Context, store *store.Store, created time.Time, metricResolution time.Duration) *Manager {
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
			logger.V(2).Printf("An error occurred while parsing expression %s: %v. This rule was not registered", val, err)
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

	//minResTime is the minimum time before actually sending
	// any alerting points. Some metrics can a take bit of time before
	// first points creation; we do not want them to be labelled as unknown on start.
	minResTime := metricResolution*2 + 10*time.Second

	rm := Manager{
		store:            store,
		recordingRules:   []*rules.Group{defaultGroup},
		alertingRules:    map[string]*ruleGroup{},
		engine:           engine,
		logger:           promLogger,
		agentStarted:     created,
		metricResolution: minResTime,
	}

	return &rm
}

//UpdateMetricResolution updates the metric resolution time.
func (rm *Manager) UpdateMetricResolution(metricResolution time.Duration) {
	rm.l.Lock()
	defer rm.l.Unlock()

	rm.metricResolution = metricResolution*2 + 10*time.Second
}

//Metriclist returns a list of all alerting rules metric names.
// This is used for dynamic generation of filters.
func (rm *Manager) MetricList() []string {
	res := make([]string, 0, len(rm.alertingRules))

	rm.l.Lock()
	defer rm.l.Unlock()

	for _, r := range rm.alertingRules {
		res = append(res, r.labels[types.LabelName])
	}

	return res
}

func (rm *Manager) Run(ctx context.Context, now time.Time) {
	res := []types.MetricPoint{}

	rm.l.Lock()

	for _, rgr := range rm.recordingRules {
		rgr.Eval(ctx, now)
	}

	for _, agr := range rm.alertingRules {
		point, err := agr.runGroup(ctx, now, rm)

		if err != nil {
			logger.V(2).Printf("An error occurred while trying to execute rules: %w")
		} else if point != nil {
			res = append(res, *point)
		}
	}

	rm.l.Unlock()

	if len(res) != 0 {
		logger.V(2).Printf("Sending %d new alert to the api", len(res))
		rm.store.PushPoints(res)
	}
}

func (agr *ruleGroup) shouldSkip(now time.Time) bool {
	if !agr.inactiveSince.IsZero() && !agr.isUserAlert {
		if agr.disabledUntil.IsZero() && now.After(agr.inactiveSince.Add(2*time.Minute)) {
			logger.V(2).Printf("rule %s has been disabled for the last 2 minutes. retrying this metric in 10 minutes", agr.labelsText)
			agr.disabledUntil = now.Add(10 * time.Minute)
		}

		if now.After(agr.disabledUntil) {
			logger.V(2).Printf("Inactive rule %s will be re executed. Time since inactive: %s", agr.labelsText, agr.inactiveSince.Format(time.RFC3339))
			agr.disabledUntil = now.Add(10 * time.Minute)
		} else {
			return true
		}
	}

	return false
}

func (agr *ruleGroup) runGroup(ctx context.Context, now time.Time, rm *Manager) (*types.MetricPoint, error) {
	thresholdOrder := []string{highCriticalState, lowCriticalState, highWarningState, lowWarningState}

	var generatedPoint *types.MetricPoint = nil

	if agr.shouldSkip(now) {
		return nil, nil
	}

	if agr.isError != "" {
		return &types.MetricPoint{
			Point: types.Point{
				Time:  now,
				Value: float64(types.StatusUnknown.NagiosCode()),
			},
			Labels: agr.labels,
			Annotations: types.MetricAnnotations{
				Status: types.StatusDescription{
					CurrentStatus:     types.StatusUnknown,
					StatusDescription: agr.unknownDescription(),
				},
			},
		}, nil
	}

	for _, val := range thresholdOrder {
		rule := agr.rules[val]

		if rule == nil {
			continue
		}

		prevState := rule.State()

		queryable := &store.CountingQueryable{Queryable: rm.store}
		_, err := rule.Eval(ctx, now, rules.EngineQueryFunc(rm.engine, queryable), nil)

		if err != nil {
			return nil, err
		}

		state := rule.State()

		if point, ret := agr.checkNoPoint(queryable, now, rm.agentStarted, rm.metricResolution); ret {
			return point, nil
		}

		agr.inactiveSince = time.Time{}
		agr.disabledUntil = time.Time{}

		// we add 20 seconds to the promAlert time to compensate the delta between agent startup and first metrics
		if state != rules.StateInactive && time.Since(rm.agentStarted) < promAlertTime+20*time.Second {
			return nil, nil
		}

		logger.V(2).Printf("metric state for %s previous state=%v, new state=%v", rule.Name(), prevState, state)

		newPoint, err := agr.generateNewPoint(val, rule, state, now)
		if err != nil {
			return nil, err
		}

		if state == rules.StateFiring {
			return newPoint, nil
		} else if generatedPoint == nil {
			generatedPoint = newPoint
		}
	}

	return generatedPoint, nil
}

func (agr *ruleGroup) checkNoPoint(queryable *store.CountingQueryable, now time.Time, agentStart time.Time, metricRes time.Duration) (*types.MetricPoint, bool) {
	if queryable.Count() == 0 {
		if agr.isUserAlert && time.Since(agentStart) > metricRes {
			return &types.MetricPoint{
				Point: types.Point{
					Time:  now,
					Value: float64(types.StatusUnknown.NagiosCode()),
				},
				Labels: agr.labels,
				Annotations: types.MetricAnnotations{
					Status: types.StatusDescription{
						CurrentStatus:     types.StatusUnknown,
						StatusDescription: agr.unknownDescription(),
					},
				},
			}, true
		}

		if agr.inactiveSince.IsZero() {
			agr.inactiveSince = now
		}

		return nil, true
	}

	return nil, false
}

func (agr *ruleGroup) unknownDescription() string {
	if agr.isError != "" {
		return "Invalid PromQL: " + agr.isError
	}

	return "PromQL read zero points. The PromQL may be incorrect or the source measurement may have disappeared"
}

func (agr *ruleGroup) generateNewPoint(thresholdType string, rule *rules.AlertingRule, state rules.AlertState, now time.Time) (*types.MetricPoint, error) {
	statusCode := statusFromThreshold(thresholdType)
	alerts := rule.ActiveAlerts()

	desc := "Current value: none."

	if len(alerts) != 0 {
		exceeded := ""

		if state != rules.StateFiring {
			exceeded = "not "
		}

		desc = fmt.Sprintf("Current value: %s. Threshold (%s) %sexceeded for the last %s",
			threshold.FormatValue(alerts[0].Value, threshold.Unit{}),
			threshold.FormatValue(agr.lowestThreshold(), threshold.Unit{}),
			exceeded, promAlertTime.String())
	}

	status := types.StatusDescription{
		CurrentStatus:     statusCode,
		StatusDescription: desc,
	}

	if statusCode == types.StatusUnknown {
		return nil, fmt.Errorf("%w for metric %s", errUnknownState, rule.Labels().String())
	}

	newPoint := types.MetricPoint{
		Point: types.Point{
			Time:  now,
			Value: float64(statusCode.NagiosCode()),
		},
		Labels: agr.labels,
		Annotations: types.MetricAnnotations{
			Status: status,
		},
	}

	if state == rules.StatePending || state == rules.StateInactive {
		statusCode = statusFromThreshold("ok")
		newPoint.Value = float64(statusCode.NagiosCode())
		newPoint.Annotations.Status.CurrentStatus = statusCode
	}

	return &newPoint, nil
}

func (agr *ruleGroup) lowestThreshold() float64 {
	orderToPrint := []string{lowWarningState, highWarningState, lowCriticalState, highCriticalState}

	for _, thr := range orderToPrint {
		_, ok := agr.rules[thr]
		if !ok {
			continue
		}

		return agr.thresholdFromString(thr)
	}

	logger.V(2).Printf("An unknown error occurred: not threshold found for rule %s\n", agr.labelsText)

	return 0
}

func (agr *ruleGroup) thresholdFromString(threshold string) float64 {
	switch threshold {
	case highCriticalState:
		return agr.thresholds.HighCritical
	case lowCriticalState:
		return agr.thresholds.LowCritical
	case highWarningState:
		return agr.thresholds.HighWarning
	case lowWarningState:
		return agr.thresholds.LowWarning
	default:
		return agr.thresholds.HighCritical
	}
}

func (rm *Manager) addAlertingRule(metric bleemeoTypes.Metric, isError string) error {
	newGroup := &ruleGroup{
		rules:         make(map[string]*rules.AlertingRule, 4),
		inactiveSince: time.Time{},
		disabledUntil: time.Time{},
		labelsText:    metric.LabelsText,
		promql:        metric.PromQLQuery,
		thresholds:    metric.Threshold.ToInternalThreshold(),
		labels:        metric.Labels,
		isUserAlert:   metric.IsUserPromQLAlert,
		isError:       isError,
	}

	if metric.Threshold.LowWarning != nil {
		err := newGroup.newRule(fmt.Sprintf("(%s) < %f", metric.PromQLQuery, *metric.Threshold.LowWarning), metric.Labels[types.LabelName], lowWarningState, "warning", rm.logger)
		if err != nil {
			return err
		}
	}

	if metric.Threshold.HighWarning != nil {
		err := newGroup.newRule(fmt.Sprintf("(%s) > %f", metric.PromQLQuery, *metric.Threshold.HighWarning), metric.Labels[types.LabelName], highWarningState, "warning", rm.logger)
		if err != nil {
			return err
		}
	}

	if metric.Threshold.LowCritical != nil {
		err := newGroup.newRule(fmt.Sprintf("(%s) < %f", metric.PromQLQuery, *metric.Threshold.LowCritical), metric.Labels[types.LabelName], lowCriticalState, "critical", rm.logger)
		if err != nil {
			return err
		}
	}

	if metric.Threshold.HighCritical != nil {
		err := newGroup.newRule(fmt.Sprintf("(%s) > %f", metric.PromQLQuery, *metric.Threshold.HighCritical), metric.Labels[types.LabelName], highCriticalState, "critical", rm.logger)
		if err != nil {
			return err
		}
	}

	rm.alertingRules[newGroup.labelsText] = newGroup

	return nil
}

func (agr *ruleGroup) Equal(threshold threshold.Threshold) bool {
	return agr.thresholds.Equal(threshold)
}

//RebuildAlertingRules rebuild the alerting rules list from a bleemeo api metric list.
func (rm *Manager) RebuildAlertingRules(metricsList []bleemeoTypes.Metric) error {
	rm.l.Lock()
	defer rm.l.Unlock()

	old := rm.alertingRules

	rm.alertingRules = make(map[string]*ruleGroup)

	for _, val := range metricsList {
		if len(val.PromQLQuery) == 0 || val.ToInternalThreshold().IsZero() {
			continue
		}

		prevInstance, ok := old[val.LabelsText]

		if ok && prevInstance.promql == val.PromQLQuery && prevInstance.Equal(val.Threshold.ToInternalThreshold()) {
			rm.alertingRules[prevInstance.labelsText] = prevInstance
		} else {
			err := rm.addAlertingRule(val, "")
			if err != nil {
				val.PromQLQuery = "not_used"
				val.IsUserPromQLAlert = true

				_ = rm.addAlertingRule(val, err.Error())
			}
		}
	}

	return nil
}

func (rm *Manager) ResetInactiveRules() {
	now := time.Now().Truncate(time.Second)

	for _, val := range rm.alertingRules {
		if val.disabledUntil.IsZero() {
			continue
		}

		if now.Add(minResetTime).Before(val.disabledUntil) {
			val.disabledUntil = now.Add(minResetTime)
		}
	}
}

func (rm *Manager) DiagnosticZip(zipFile *zip.Writer) error {
	file, err := zipFile.Create("alertings-recording-rules.txt")
	if err != nil {
		return err
	}

	rm.l.Lock()
	defer rm.l.Unlock()

	fmt.Fprintf(file, "# Recording rules (%d entries)\n", len(rm.recordingRules))

	for _, gr := range rm.recordingRules {
		fmt.Fprintf(file, "# group %s\n", gr.Name())

		for _, r := range gr.Rules() {
			fmt.Fprintf(file, "%s\n", r.String())
		}
	}

	activeAlertingRules := 0

	for _, r := range rm.alertingRules {
		if r.inactiveSince.IsZero() {
			activeAlertingRules++
		}
	}

	fmt.Fprintf(file, "# Active Alerting rules (%d entries)\n", activeAlertingRules)

	for _, r := range rm.alertingRules {
		if r.inactiveSince.IsZero() {
			fmt.Fprintf(file, "%s\n", r.String())
		}
	}

	fmt.Fprintf(file, "\n# Inactive Alerting Rules (%d entries)\n", len(rm.alertingRules)-activeAlertingRules)

	for _, r := range rm.alertingRules {
		if !r.inactiveSince.IsZero() {
			fmt.Fprintf(file, "%s\n", r.String())
		}
	}

	return nil
}

func (agr *ruleGroup) newRule(exp string, metricName string, threshold string, severity string, logger log.Logger) error {
	newExp, err := parser.ParseExpr(exp)
	if err != nil {
		return err
	}

	newRule := rules.NewAlertingRule(metricName+"_"+threshold,
		newExp, promAlertTime, nil, labels.Labels{labels.Label{Name: "severity", Value: severity}},
		labels.Labels{}, true, log.With(logger, "alerting_rule", metricName+"_"+threshold))

	agr.rules[threshold] = newRule

	return nil
}

func (agr *ruleGroup) String() string {
	return fmt.Sprintf("id=%s query=%#v inactive_since=%v disabled_until=%v is_user_promql_alert=%v\n%v",
		agr.labelsText, agr.promql, agr.inactiveSince, agr.disabledUntil, agr.isUserAlert, agr.Query())
}

func (agr *ruleGroup) Query() string {
	res := ""

	for key, val := range agr.rules {
		res += fmt.Sprintf("\tThreshold_%s: %s\n", key, val.Query().String())
	}

	return res
}

func statusFromThreshold(s string) types.Status {
	switch s {
	case "ok":
		return types.StatusOk
	case lowWarningState:
	case highWarningState:
		return types.StatusWarning
	case lowCriticalState:
	case highCriticalState:
		return types.StatusCritical
	default:
		return types.StatusUnknown
	}

	return types.StatusUnknown
}
