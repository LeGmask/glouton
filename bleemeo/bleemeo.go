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

package bleemeo

import (
	"archive/zip"
	"context"
	"fmt"
	"glouton/bleemeo/internal/cache"
	"glouton/bleemeo/internal/mqtt"
	"glouton/bleemeo/internal/synchronizer"
	"glouton/bleemeo/types"
	"glouton/logger"
	"io"
	"math/rand"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	gloutonTypes "glouton/types"
)

// Connector manager the connection between the Agent and Bleemeo.
type Connector struct {
	option types.GlobalOption

	cache       *cache.Cache
	sync        *synchronizer.Synchronizer
	mqtt        *mqtt.Client
	mqttRestart chan interface{}

	l               sync.RWMutex
	lastKnownReport time.Time
	lastMQTTRestart time.Time
	disabledUntil   time.Time
	disableReason   types.DisableReason

	// initialized indicates whether the mqtt connetcor can be started
	initialized bool
}

// New create a new Connector.
func New(option types.GlobalOption) (c *Connector, err error) {
	c = &Connector{
		option:      option,
		cache:       cache.Load(option.State),
		mqttRestart: make(chan interface{}, 1),
	}
	c.sync, err = synchronizer.New(synchronizer.Option{
		GlobalOption:                c.option,
		Cache:                       c.cache,
		UpdateConfigCallback:        c.updateConfig,
		DisableCallback:             c.disableCallback,
		SetInitialized:              c.setInitialized,
		SetBleemeoInMaintenanceMode: c.setMaintenance,
		IsMqttConnected:             c.Connected,
	})

	return c, err
}

func (c *Connector) setInitialized() {
	c.l.Lock()
	defer c.l.Unlock()

	c.initialized = true
}

func (c *Connector) isInitialized() bool {
	c.l.RLock()
	defer c.l.RUnlock()

	return c.initialized
}

// ApplyCachedConfiguration reload metrics units & threshold & monitors from the cache.
func (c *Connector) ApplyCachedConfiguration() {
	c.l.RLock()
	disabledUntil := c.disabledUntil
	defer c.l.RUnlock()

	if time.Now().Before(disabledUntil) {
		return
	}

	c.sync.UpdateUnitsAndThresholds(true)

	if c.option.Config.Bool("blackbox.enable") {
		if err := c.sync.ApplyMonitorUpdate(); err != nil {
			// we just log the error, as we will try to run the monitors later anyway
			logger.V(2).Printf("Couldn't start probes now, will retry later: %v", err)
		}
	}

	currentConfig := c.cache.CurrentAccountConfig()

	if c.option.UpdateMetricResolution != nil && currentConfig.MetricAgentResolution != 0 {
		c.option.UpdateMetricResolution(time.Duration(currentConfig.MetricAgentResolution) * time.Second)
	}
}

func (c *Connector) initMQTT(previousPoint []gloutonTypes.MetricPoint, first bool) error {
	c.l.Lock()
	defer c.l.Unlock()

	var password string

	err := c.option.State.Get("password", &password)
	if err != nil {
		return err
	}

	c.mqtt = mqtt.New(
		mqtt.Option{
			GlobalOption:         c.option,
			Cache:                c.cache,
			AgentID:              types.AgentID(c.AgentID()),
			AgentPassword:        password,
			UpdateConfigCallback: c.sync.NotifyConfigUpdate,
			UpdateMetrics:        c.sync.UpdateMetrics,
			UpdateMaintenance:    c.sync.UpdateMaintenance,
			UpdateMonitor:        c.sync.UpdateMonitor,
			InitialPoints:        previousPoint,
		},
		first,
	)

	// if the connector is disabled, disable mqtt for the same period
	if c.disabledUntil.After(time.Now()) {
		c.disableMqtt(c.mqtt, c.disableReason, c.disabledUntil)
	}

	if c.sync.IsMaintenance() {
		c.mqtt.SuspendSending(true)
	}

	return nil
}

func (c *Connector) setMaintenance(maintenance bool) {
	if maintenance {
		logger.V(0).Println("Bleemeo: read only/maintenance mode enabled")
	} else if !maintenance && c.sync.IsMaintenance() {
		logger.V(0).Println("Bleemeo: read only/maintenance mode is now disabled, will resume sending metrics")
	}

	c.l.RLock()
	defer c.l.RUnlock()

	c.sync.SetMaintenance(maintenance)

	if c.mqtt != nil {
		c.mqtt.SuspendSending(maintenance)
	}
}

func (c *Connector) mqttRestarter(ctx context.Context) error {
	var (
		wg             sync.WaitGroup
		mqttErr        error
		l              sync.Mutex
		previousPoints []gloutonTypes.MetricPoint
		alreadyInit    bool
	)

	subCtx, cancel := context.WithCancel(ctx)

	c.l.RLock()
	mqttRestart := c.mqttRestart
	c.l.RUnlock()

	if mqttRestart == nil {
		return nil
	}

	select {
	case mqttRestart <- nil:
	default:
	}

	for range mqttRestart {
		cancel()

		subCtx, cancel = context.WithCancel(ctx)

		c.l.Lock()

		if c.mqtt != nil {
			// Try to retrieve pending points
			resultChan := make(chan []gloutonTypes.MetricPoint, 1)

			go func() {
				resultChan <- c.mqtt.PopPoints(true)
			}()

			select {
			case previousPoints = <-resultChan:
			case <-time.After(10 * time.Second):
			}
		}

		c.mqtt = nil

		c.l.Unlock()

		err := c.initMQTT(previousPoints, !alreadyInit)
		previousPoints = nil
		alreadyInit = true

		if err != nil {
			l.Lock()

			if mqttErr == nil {
				mqttErr = err
			}

			l.Unlock()

			break
		}

		wg.Add(1)

		go func() {
			defer wg.Done()

			err := c.mqtt.Run(subCtx)

			l.Lock()

			if mqttErr == nil {
				mqttErr = err
			}

			l.Unlock()
		}()
	}

	cancel()
	wg.Wait()

	return mqttErr
}

// Run run the Connector.
func (c *Connector) Run(ctx context.Context) error {
	defer c.cache.Save()

	subCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		wg               sync.WaitGroup
		syncErr, mqttErr error
	)

	wg.Add(1)

	go func() {
		defer wg.Done()
		defer cancel()

		syncErr = c.sync.Run(subCtx)
	}()

	wg.Add(1)

	go func() {
		defer wg.Done()
		defer cancel()

		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for subCtx.Err() == nil {
			c.emitInternalMetric()

			select {
			case <-ticker.C:
			case <-subCtx.Done():
			}
		}

		c.l.Lock()
		close(c.mqttRestart)
		c.mqttRestart = nil
		c.l.Unlock()

		logger.V(2).Printf("Bleemeo connector stopping")
	}()

	for subCtx.Err() == nil {
		if c.AgentID() != "" && c.isInitialized() {
			wg.Add(1)

			go func() {
				defer wg.Done()
				defer cancel()

				mqttErr = c.mqttRestarter(subCtx)
			}()

			break
		}

		select {
		case <-time.After(5 * time.Second):
		case <-subCtx.Done():
		}
	}

	wg.Wait()
	logger.V(2).Printf("Bleemeo connector stopped")

	if syncErr != nil {
		return syncErr
	}

	return mqttErr
}

// UpdateContainers request to update a containers.
func (c *Connector) UpdateContainers() {
	c.l.RLock()

	disabled := time.Now().Before(c.disabledUntil)

	c.l.RUnlock()

	if disabled {
		return
	}

	c.sync.UpdateContainers()
}

// UpdateInfo request to update a info, which include the time_drift.
func (c *Connector) UpdateInfo() {
	// It's updateInfo which disable for time drift. Temporary re-enable to
	// run it.
	c.clearDisable(types.DisableTimeDrift)

	c.l.RLock()

	disabled := time.Now().Before(c.disabledUntil)

	c.l.RUnlock()

	if disabled {
		return
	}

	c.sync.UpdateInfo()
}

// UpdateMonitors trigger a reload of the monitors.
func (c *Connector) UpdateMonitors() {
	c.sync.UpdateMonitors()
}

func (c *Connector) RelabelHook(labels map[string]string) (newLabel map[string]string, retryLater bool) {
	agentID := c.AgentID()

	if agentID == "" {
		return labels, false
	}

	labels[gloutonTypes.LabelMetaBleemeoUUID] = agentID

	if labels[gloutonTypes.LabelMetaSNMPTarget] != "" {
		var (
			snmpTypeID string
			found      bool
		)

		for _, t := range c.cache.AgentTypes() {
			if t.Name == types.AgentTypeSNMP {
				snmpTypeID = t.ID

				break
			}
		}

		for _, a := range c.cache.Agents() {
			if a.AgentType == snmpTypeID && a.FQDN == labels[gloutonTypes.LabelMetaSNMPTarget] {
				labels[gloutonTypes.LabelMetaBleemeoTargetAgentUUID] = a.ID
				found = true

				break
			}
		}

		if !found {
			// set retryLater which will cause metrics from the gatherer to be ignored.
			// This hook will be automatically re-called every 2 minutes.
			return labels, true
		}
	}

	return labels, false
}

// DiagnosticPage return useful information to troubleshoot issue.
func (c *Connector) DiagnosticPage() string {
	builder := &strings.Builder{}

	registrationKey := []rune(c.option.Config.String("bleemeo.registration_key"))
	for i := range registrationKey {
		if i >= 6 && i < len(registrationKey)-4 {
			registrationKey[i] = '*'
		}
	}

	fmt.Fprintf(
		builder,
		"Bleemeo account ID is %#v and registration key is %#v\n",
		c.AccountID(), string(registrationKey),
	)

	if c.AgentID() == "" {
		fmt.Fprintln(builder, "Glouton is not registered with Bleemeo")
	} else {
		fmt.Fprintf(builder, "Glouton is registered with Bleemeo with ID %v\n", c.AgentID())
	}

	lastReport := c.LastReport().Format(time.RFC3339)

	if c.Connected() {
		fmt.Fprintf(builder, "Glouton is currently connected. Last report to Bleemeo at %s\n", lastReport)
	} else {
		fmt.Fprintf(builder, "Glouton is currently NOT connected. Last report to Bleemeo at %s\n", lastReport)
	}

	c.l.Lock()
	if time.Now().Before(c.disabledUntil) {
		fmt.Fprintf(
			builder,
			"Glouton connection to Bleemeo is disabled until %s (%v remain) due to '%v'\n",
			c.disabledUntil.Format(time.RFC3339),
			time.Until(c.disabledUntil).Truncate(time.Second),
			c.disableReason,
		)
	}

	if now := time.Now(); c.disabledUntil.After(now) {
		fmt.Fprintf(builder, "The Bleemeo connector is currently disabled until %v due to '%v'\n", c.disabledUntil, c.disableReason)
	}

	if c.sync.IsMaintenance() {
		fmt.Fprintln(builder, "The Bleemeo connector is currently in read-only/maintenance mode, not syncing nor sending any metric")
	}

	mqtt := c.mqtt
	c.l.Unlock()

	syncPage := make(chan string)
	mqttPage := make(chan string)

	go func() {
		syncPage <- c.sync.DiagnosticPage()
	}()

	go func() {
		if mqtt == nil {
			mqttPage <- "MQTT connector is not (yet) initialized\n"
		} else {
			mqttPage <- c.mqtt.DiagnosticPage()
		}
	}()

	builder.WriteString(<-syncPage)
	builder.WriteString(<-mqttPage)

	return builder.String()
}

// DiagnosticZip add to a zipfile useful diagnostic information.
func (c *Connector) DiagnosticZip(zipFile *zip.Writer) error {
	c.l.Lock()
	mqtt := c.mqtt
	c.l.Unlock()

	if mqtt != nil {
		if err := mqtt.DiagnosticZip(zipFile); err != nil {
			return err
		}
	}

	failed := c.cache.MetricRegistrationsFail()
	if len(failed) > 0 {
		file, err := zipFile.Create("metric-registration-failed.txt")
		if err != nil {
			return err
		}

		indices := make([]int, len(failed))
		for i := range indices {
			indices[i] = i
		}

		const maxSample = 50
		if len(failed) > maxSample {
			fmt.Fprintf(file, "%d metrics fail to register. The following is 50 randomly choose metrics that fail:\n", len(failed))
			indices = rand.Perm(len(failed))[:maxSample]
		} else {
			fmt.Fprintf(file, "%d metrics fail to register. The following is the fill list\n", len(failed))
			sort.Slice(indices, func(i, j int) bool {
				return failed[indices[i]].LabelsText < failed[indices[j]].LabelsText
			})
		}

		for _, i := range indices {
			row := failed[i]
			fmt.Fprintf(file, "count=%d nextRetryAt=%s failureKind=%v labels=%s\n", row.FailCounter, row.RetryAfter().Format(time.RFC3339), row.LastFailKind, row.LabelsText)
		}
	}

	file, err := zipFile.Create("bleemeo-cache.txt")
	if err != nil {
		return err
	}

	c.diagnosticCache(file)

	return nil
}

func (c *Connector) diagnosticCache(file io.Writer) {
	agents := c.cache.Agents()
	agentTypes := c.cache.AgentTypes()

	fmt.Fprintf(file, "# Cache known %d agents\n", len(agents))

	for _, a := range agents {
		agentTypeName := ""

		for _, t := range agentTypes {
			if t.ID == a.AgentType {
				agentTypeName = t.DisplayName

				break
			}
		}

		fmt.Fprintf(file, "id=%s fqdn=%s type=%s (%s) accountID=%s, config=%s\n", a.ID, a.FQDN, agentTypeName, a.AgentType, a.AccountID, a.CurrentConfigID)
	}

	fmt.Fprintf(file, "\n# Cache known %d agent types\n", len(agentTypes))

	for _, t := range agentTypes {
		fmt.Fprintf(file, "id=%s name=%s display_name=%s\n", t.ID, t.Name, t.DisplayName)
	}

	metrics := c.cache.Metrics()
	activeMetrics := 0

	for _, m := range metrics {
		if m.DeactivatedAt.IsZero() {
			activeMetrics++
		}
	}

	accountConfigs := c.cache.AccountConfigsByUUID()

	fmt.Fprintf(file, "\n# Cache known %d account config\n", len(accountConfigs))

	for _, ac := range accountConfigs {
		fmt.Fprintf(file, "%#v\n", ac)
	}

	fmt.Fprintf(file, "# And current account config is\n")
	fmt.Fprintf(file, "%#v\n", c.cache.CurrentAccountConfig())

	fmt.Fprintf(file, "\n# Cache known %d metrics and %d active metrics\n", len(metrics), activeMetrics)
	fmt.Fprintf(file, "\n# Cache known %d facts\n", len(c.cache.Facts()))
	fmt.Fprintf(file, "\n# Cache known %d services\n", len(c.cache.Services()))
	fmt.Fprintf(file, "\n# Cache known %d containers\n", len(c.cache.Containers()))
	fmt.Fprintf(file, "\n# Cache known %d monitors\n", len(c.cache.Monitors()))
}

// Tags returns the Tags set on Bleemeo Cloud platform.
func (c *Connector) Tags() []string {
	agent := c.cache.Agent()
	tags := make([]string, len(agent.Tags))

	for i, t := range agent.Tags {
		tags[i] = t.Name
	}

	return tags
}

// AccountID returns the Account UUID of Bleemeo
// It return the empty string if the Account UUID is not available.
func (c *Connector) AccountID() string {
	c.l.Lock()
	defer c.l.Unlock()

	accountID := c.cache.AccountID()
	if accountID != "" {
		return accountID
	}

	return c.option.Config.String("bleemeo.account_id")
}

// AgentID returns the Agent UUID of Bleemeo
// It return the empty string if the Account UUID is not available.
func (c *Connector) AgentID() string {
	var agentID string

	err := c.option.State.Get("agent_uuid", &agentID)
	if err != nil {
		return ""
	}

	return agentID
}

// RegistrationAt returns the date of registration with Bleemeo API.
func (c *Connector) RegistrationAt() time.Time {
	c.l.RLock()
	defer c.l.RUnlock()

	agent := c.cache.Agent()

	return agent.CreatedAt
}

// Connected returns whether the mqtt connector is connected.
func (c *Connector) Connected() bool {
	c.l.RLock()
	defer c.l.RUnlock()

	if c.mqtt == nil {
		return false
	}

	return c.mqtt.Connected()
}

// LastReport returns the date of last report with Bleemeo API over MQTT.
func (c *Connector) LastReport() time.Time {
	c.l.Lock()
	defer c.l.Unlock()

	if c.mqtt != nil {
		tmp := c.mqtt.LastReport()
		if tmp.After(c.lastKnownReport) {
			c.lastKnownReport = tmp
		}
	}

	return c.lastKnownReport
}

// HealthCheck perform some health check and logger any issue found.
func (c *Connector) HealthCheck() bool {
	ok := true

	if c.AgentID() == "" {
		logger.Printf("Agent not yet registered")

		ok = false
	}

	lastReport := c.LastReport()

	c.l.Lock()
	defer c.l.Unlock()

	if time.Now().Before(c.disabledUntil) {
		delay := time.Until(c.disabledUntil)

		logger.Printf("Bleemeo connector is still disabled for %v due to '%v'", delay.Truncate(time.Second), c.disableReason)

		return false
	}

	if c.mqtt != nil {
		ok = c.mqtt.HealthCheck() && ok

		if !lastReport.IsZero() && time.Since(lastReport) > time.Hour && (c.lastMQTTRestart.IsZero() || time.Since(c.lastMQTTRestart) > 4*time.Hour) {
			c.lastMQTTRestart = time.Now()

			logger.Printf("MQTT connection fail to re-establish since %s. This may be a long network issue or a Glouton bug", lastReport.Format(time.RFC3339))

			if time.Since(lastReport) > 36*time.Hour {
				logger.Printf("Restarting MQTT is not enough. Glouton seems unhealthy, killing mysel")

				// We don't know how big the buffer needs to be to collect
				// all the goroutines. Use 2MB buffer which hopefully is enough
				buffer := make([]byte, 1<<21)

				runtime.Stack(buffer, true)
				logger.Printf("%s", string(buffer))
				panic("Glouton seems unhealthy, killing myself")
			}

			logger.Printf("Trying to restart the MQTT connection from scratch")

			if c.mqttRestart != nil {
				c.mqttRestart <- nil
			}
		}
	}

	return ok
}

func (c *Connector) emitInternalMetric() {
	c.l.RLock()
	defer c.l.RUnlock()

	if c.mqtt != nil && c.mqtt.Connected() {
		c.option.Acc.AddFields("", map[string]interface{}{"agent_status": 1.0}, nil, time.Now().Truncate(time.Second))
	}
}

func (c *Connector) updateConfig() {
	currentConfig := c.cache.CurrentAccountConfig()

	logger.Printf("Changed to configuration %s", currentConfig.Name)

	if c.option.UpdateMetricResolution != nil {
		c.option.UpdateMetricResolution(time.Duration(currentConfig.MetricAgentResolution) * time.Second)
	}
}

func (c *Connector) clearDisable(reasonToClear types.DisableReason) {
	c.l.Lock()

	if time.Now().Before(c.disabledUntil) && c.disableReason == reasonToClear {
		c.disabledUntil = time.Now()
	}

	c.l.Unlock()
	c.sync.ClearDisable(reasonToClear, 0)

	if mqtt := c.mqtt; mqtt != nil {
		var mqttDisableDelay time.Duration

		switch reasonToClear { //nolint:exhaustive
		case types.DisableTooManyErrors:
			mqttDisableDelay = 20 * time.Second
		case types.DisableAgentTooOld, types.DisableDuplicatedAgent, types.DisableAuthenticationError, types.DisableTimeDrift:
			// give time to the synchronizer check if the error is solved
			mqttDisableDelay = 80 * time.Second
		default:
			mqttDisableDelay = 20 * time.Second
		}

		mqtt.ClearDisable(reasonToClear, mqttDisableDelay)
	}
}

func (c *Connector) disableCallback(reason types.DisableReason, until time.Time) {
	c.l.Lock()

	if c.disabledUntil.After(until) {
		return
	}

	c.disabledUntil = until
	c.disableReason = reason

	mqtt := c.mqtt

	c.l.Unlock()

	delay := time.Until(until)

	logger.Printf("Disabling Bleemeo connector for %v due to '%v'", delay.Truncate(time.Second), reason)
	c.sync.Disable(until, reason)

	c.disableMqtt(mqtt, reason, until)
}

func (c *Connector) disableMqtt(mqtt *mqtt.Client, reason types.DisableReason, until time.Time) {
	if mqtt != nil {
		// delay to apply between re-enabling the synchronizer and the mqtt client. The goal is to allow for
		// the synchronizer to disable mqtt again before mqtt have time to reconnect or send metrics.
		var mqttDisableDelay time.Duration

		switch reason { //nolint:exhaustive
		case types.DisableTooManyErrors:
			mqttDisableDelay = 20 * time.Second
		case types.DisableAgentTooOld, types.DisableDuplicatedAgent, types.DisableAuthenticationError, types.DisableTimeDrift:
			// give time to the synchronizer check if the error is solved
			mqttDisableDelay = 80 * time.Second
		default:
			mqttDisableDelay = 20 * time.Second
		}

		mqtt.Disable(until.Add(mqttDisableDelay), reason)
	}
}
