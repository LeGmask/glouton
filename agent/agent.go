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

// Package agent contains the glue between other components
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"glouton/agent/state"
	"glouton/api"
	"glouton/bleemeo"
	"glouton/collector"
	"glouton/config"
	"glouton/debouncer"
	"glouton/delay"
	"glouton/discovery"
	"glouton/discovery/promexporter"
	"glouton/facts"
	"glouton/facts/container-runtime/containerd"
	"glouton/facts/container-runtime/kubernetes"
	"glouton/facts/container-runtime/merge"
	"glouton/influxdb"
	"glouton/inputs"
	"glouton/inputs/docker"
	"glouton/inputs/statsd"
	"glouton/jmxtrans"
	"glouton/logger"
	"glouton/nrpe"
	"glouton/prometheus/exporter/blackbox"
	"glouton/prometheus/exporter/common"
	"glouton/prometheus/exporter/snmp"
	"glouton/prometheus/process"
	"glouton/prometheus/registry"
	"glouton/prometheus/rules"
	"glouton/prometheus/scrapper"
	"glouton/store"
	"glouton/task"
	"glouton/telemetry"
	"glouton/threshold"
	"glouton/types"
	"glouton/version"
	"glouton/zabbix"
	"io/ioutil"
	"log"
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/getsentry/sentry-go"

	bleemeoTypes "glouton/bleemeo/types"

	dockerRuntime "glouton/facts/container-runtime/docker"

	crTypes "glouton/facts/container-runtime/types"

	processInput "glouton/inputs/process"

	"github.com/google/uuid"
	"github.com/prometheus/prometheus/pkg/labels"
	"gopkg.in/yaml.v3"
)

// Jitter define the aligned timestamp used for scrapping.
// System collector use 0 (baseJitter here and in registry.go).
// baseJitterPlus is a little after, useful for collector that need to re-read point of system collector.
const (
	baseJitter      = 0
	baseJitterPlus  = 500000
	defaultInterval = 0
)

var errUnsupportedKey = errors.New("Unsupported item key") //nolint:stylecheck

type agent struct {
	taskRegistry *task.Registry
	oldConfig    *config.Configuration
	config       Config
	state        *state.State
	cancel       context.CancelFunc
	context      context.Context

	hostRootPath           string
	discovery              *discovery.Discovery
	dockerRuntime          *dockerRuntime.Docker
	containerdRuntime      *containerd.Containerd
	containerRuntime       crTypes.RuntimeInterface
	collector              *collector.Collector
	factProvider           *facts.FactProvider
	bleemeoConnector       *bleemeo.Connector
	influxdbConnector      *influxdb.Client
	threshold              *threshold.Registry
	jmx                    *jmxtrans.JMX
	snmpManager            *snmp.Manager
	snmpRegistration       []int
	store                  *store.Store
	gathererRegistry       *registry.Registry
	metricFormat           types.MetricFormat
	dynamicScrapper        *promexporter.DynamicScrapper
	lastHealCheck          time.Time
	lastContainerEventTime time.Time
	watchdogRunAt          []time.Time
	metricFilter           *metricFilter
	monitorManager         *blackbox.RegisterManager

	triggerHandler            *debouncer.Debouncer
	triggerLock               sync.Mutex
	triggerDiscAt             time.Time
	triggerDiscImmediate      bool
	triggerFact               bool
	triggerSystemUpdateMetric bool

	dockerInputPresent bool
	dockerInputID      int

	l                sync.Mutex
	taskIDs          map[string]int
	metricResolution time.Duration
}

func zabbixResponse(key string, args []string) (string, error) {
	if key == "agent.ping" {
		return "1", nil
	}

	if key == "agent.version" {
		return fmt.Sprintf("4 (Glouton %s)", version.Version), nil
	}

	return "", errUnsupportedKey
}

type taskInfo struct {
	function task.Runner
	name     string
}

func (a *agent) init(configFiles []string) (ok bool) {
	a.l.Lock()
	a.lastHealCheck = time.Now()
	a.l.Unlock()

	a.taskRegistry = task.NewRegistry(context.Background())
	cfg, oldCfg, warnings, err := LoadConfiguration(configFiles, nil)
	a.oldConfig = oldCfg
	a.config = cfg

	a.setupLogger()

	if err != nil {
		logger.Printf("Error while loading configuration: %v", err)

		return false
	}

	if dsn := a.oldConfig.String("bleemeo.sentry.dsn"); dsn != "" {
		err := sentry.Init(sentry.ClientOptions{
			Dsn: dsn,
		})
		if err != nil {
			logger.V(1).Printf("sentry.Init failed: %s", err)
		}
	}

	sentry.ConfigureScope(func(scope *sentry.Scope) {
		scope.SetContext("agent", map[string]interface{}{
			"glouton_version": version.Version,
		})
	})

	for _, w := range warnings {
		logger.Printf("Warning while loading configuration: %v", w)
	}

	statePath := a.oldConfig.String("agent.state_file")
	oldStatePath := a.oldConfig.String("agent.deprecated_state_file")

	a.state, err = state.Load(statePath)
	if err != nil {
		logger.Printf("Error while loading state file: %v", err)

		return false
	}

	if !a.state.IsEmpty() {
		oldStatePath = ""
	}

	if oldStatePath != "" {
		oldState, err := state.Load(oldStatePath)
		if err != nil {
			logger.Printf("Error while loading state file: %v", err)

			return false
		}

		if oldState.IsEmpty() {
			oldStatePath = ""
		} else {
			a.state = oldState
		}
	}

	a.migrateState()

	if err := a.state.SaveTo(statePath); err != nil {
		if oldStatePath != "" {
			stateDir := filepath.Dir(statePath)
			logger.Printf("State file can't we wrote at new path (%s): %v", statePath, err)
			logger.Printf("Keeping the deprecated path (%s).", oldStatePath)
			logger.Printf(
				"To migrate to new path, simply create a persistent folder %s or move %s to %s while Glouton is stopped",
				stateDir,
				oldStatePath,
				statePath,
			)

			err = a.state.SaveTo(oldStatePath)
		}

		if err != nil {
			logger.Printf("State file is not writable, stopping agent: %v", err)

			return false
		}
	} else if oldStatePath != "" {
		logger.Printf("The deprecated state file (%s) is migrated to new path (%s).", oldStatePath, statePath)
	}

	return true
}

func (a *agent) setupLogger() {
	logger.SetBufferCapacity(
		a.oldConfig.Int("logging.buffer.head_size_bytes"),
		a.oldConfig.Int("logging.buffer.tail_size_bytes"),
	)

	var err error

	switch a.oldConfig.String("logging.output") {
	case "syslog":
		err = logger.UseSyslog()
	case "file":
		err = logger.UseFile(a.oldConfig.String("logging.filename"))
	}

	if err != nil {
		fmt.Printf("Unable to use logging backend '%s': %v\n", a.oldConfig.String("logging.output"), err) //nolint:forbidigo
	}

	if level := a.oldConfig.Int("logging.level"); level != 0 {
		logger.SetLevel(level)
	} else {
		switch strings.ToLower(a.oldConfig.String("logging.level")) {
		case "0", "info", "warning", "error":
			logger.SetLevel(0)
		case "verbose":
			logger.SetLevel(1)
		case "debug":
			logger.SetLevel(2)
		default:
			logger.SetLevel(0)
			logger.Printf("Unknown logging.level = %#v. Using \"INFO\"", a.oldConfig.String("logging.level"))
		}
	}

	logger.SetPkgLevels(a.oldConfig.String("logging.package_levels"))
}

// Run runs Glouton.
func Run(stopCtx context.Context, reloadCtx context.Context, wgReload *sync.WaitGroup, wgStop *sync.WaitGroup, configFiles []string) {
	rand.Seed(time.Now().UnixNano())

	agent := &agent{
		taskRegistry: task.NewRegistry(context.Background()),
		taskIDs:      make(map[string]int),
	}

	agent.initOSSpecificParts()

	if !agent.init(configFiles) {
		os.Exit(1)

		return
	}

	agent.run(stopCtx, reloadCtx, wgStop)
	wgReload.Done()
}

// BleemeoAccountID returns the Account UUID of Bleemeo
// It return the empty string if the Account UUID is not available (e.g. because Bleemeo is disabled or mis-configured).
func (a *agent) BleemeoAccountID() string {
	if a.bleemeoConnector == nil {
		return ""
	}

	return a.bleemeoConnector.AccountID()
}

// BleemeoAgentID returns the Agent UUID of Bleemeo
// It return the empty string if the Agent UUID is not available (e.g. because Bleemeo is disabled or registration didn't happen yet).
func (a *agent) BleemeoAgentID() string {
	if a.bleemeoConnector == nil {
		return ""
	}

	return a.bleemeoConnector.AgentID()
}

// BleemeoRegistrationAt returns the date of Agent registration with Bleemeo API
// It return the zero time if registration didn't occurred yet.
func (a *agent) BleemeoRegistrationAt() time.Time {
	if a.bleemeoConnector == nil {
		return time.Time{}
	}

	return a.bleemeoConnector.RegistrationAt()
}

// BleemeoLastReport returns the date of last report with Bleemeo API
// It return the zero time if registration didn't occurred yet or no data send to Bleemeo API.
func (a *agent) BleemeoLastReport() time.Time {
	if a.bleemeoConnector == nil {
		return time.Time{}
	}

	return a.bleemeoConnector.LastReport()
}

// BleemeoConnected returns true if Bleemeo is currently connected (to MQTT).
func (a *agent) BleemeoConnected() bool {
	if a.bleemeoConnector == nil {
		return false
	}

	return a.bleemeoConnector.Connected()
}

// Tags returns tags of this Agent.
func (a *agent) Tags() []string {
	tagsSet := make(map[string]bool)

	for _, t := range a.oldConfig.StringList("tags") {
		tagsSet[t] = true
	}

	if a.bleemeoConnector != nil {
		for _, t := range a.bleemeoConnector.Tags() {
			tagsSet[t] = true
		}
	}

	tags := make([]string, 0, len(tagsSet))

	for t := range tagsSet {
		tags = append(tags, t)
	}

	return tags
}

// UpdateThresholds update the thresholds definition.
// This method will merge with threshold definition present in configuration file.
func (a *agent) UpdateThresholds(thresholds map[threshold.MetricNameItem]threshold.Threshold, firstUpdate bool) {
	a.updateThresholds(thresholds, firstUpdate)
}

// notifyBleemeoFirstRegistration is called when Glouton is registered with Bleemeo Cloud platform for the first time
// This means that when this function is called, BleemeoAgentID and BleemeoAccountID are set.
func (a *agent) notifyBleemeoFirstRegistration(ctx context.Context) {
	a.gathererRegistry.UpdateRelabelHook(ctx, a.bleemeoConnector.RelabelHook)
	a.store.DropAllMetrics()
}

// notifyBleemeoUpdateLabels is called when Labels might change for some metrics.
// This likely happen when SNMP target are deleted/recreated.
func (a *agent) notifyBleemeoUpdateLabels(ctx context.Context) {
	a.gathererRegistry.UpdateRelabelHook(ctx, a.bleemeoConnector.RelabelHook)
}

func (a *agent) updateSNMPResolution(resolution time.Duration) {
	a.l.Lock()
	defer a.l.Unlock()

	for _, id := range a.snmpRegistration {
		a.gathererRegistry.Unregister(id)
	}

	if a.snmpRegistration != nil {
		a.snmpRegistration = a.snmpRegistration[:0]
	}

	if resolution == 0 {
		return
	}

	for _, target := range a.snmpManager.Gatherers() {
		hash := labels.FromMap(target.ExtraLabels).Hash()

		id, err := a.gathererRegistry.RegisterGatherer(
			registry.RegistrationOption{
				Description: "snmp target " + target.Address,
				JitterSeed:  hash,
				Interval:    resolution,
				Timeout:     40 * time.Second,
				ExtraLabels: target.ExtraLabels,
				Rules:       registry.DefaultSNMPRules(),
			},
			target.Gatherer,
			true,
		)
		if err != nil {
			logger.Printf("Unable to add SNMP scrapper for target %s: %v", target.Address, err)
		} else {
			a.snmpRegistration = append(a.snmpRegistration, id)
		}
	}
}

func (a *agent) updateMetricResolution(defaultResolution time.Duration, snmpResolution time.Duration) {
	a.l.Lock()
	a.metricResolution = defaultResolution
	a.l.Unlock()

	a.gathererRegistry.UpdateDelay(defaultResolution)

	services, err := a.discovery.Discovery(a.context, time.Hour)
	if err != nil {
		logger.V(1).Printf("error during discovery: %v", err)
	} else if a.jmx != nil {
		if err := a.jmx.UpdateConfig(services, defaultResolution); err != nil {
			logger.V(1).Printf("failed to update JMX configuration: %v", err)
		}
	}

	a.updateSNMPResolution(snmpResolution)
}

func (a *agent) getConfigThreshold(firstUpdate bool) map[string]threshold.Threshold {
	rawValue, ok := a.oldConfig.Get("thresholds")
	if !ok {
		rawValue = map[string]interface{}{}
	}

	rawThreshold, ok := rawValue.(map[string]interface{})
	if !ok {
		if firstUpdate {
			logger.V(1).Printf("Threshold in configuration file is not map")
		}

		return make(map[string]threshold.Threshold)
	}

	configThreshold := make(map[string]threshold.Threshold, len(rawThreshold))

	for k, v := range rawThreshold {
		v2, ok := v.(map[string]interface{})
		if !ok {
			if firstUpdate {
				logger.V(1).Printf("Threshold in configuration file is not well-formated: %v value is not a map", k)
			}

			continue
		}

		t, err := threshold.FromInterfaceMap(v2)
		if err != nil {
			if firstUpdate {
				logger.V(1).Printf("Threshold in configuration file is not well-formated: %v", err)
			}

			continue
		}

		configThreshold[k] = t
	}

	return configThreshold
}

func (a *agent) updateThresholds(thresholds map[threshold.MetricNameItem]threshold.Threshold, firstUpdate bool) {
	configThreshold := a.getConfigThreshold(firstUpdate)

	oldThresholds := map[string]threshold.Threshold{}

	for _, name := range []string{"system_pending_updates", "system_pending_security_updates", "time_drift"} {
		key := threshold.MetricNameItem{
			Name: name,
			Item: "",
		}
		oldThresholds[name] = a.threshold.GetThreshold(key)
	}

	a.threshold.SetThresholds(thresholds, configThreshold)

	ctx := context.Background()
	services, err := a.discovery.Discovery(ctx, 1*time.Hour)

	if err != nil {
		logger.V(2).Printf("An error occurred while running discoveries for updateThresholds: %v", err)
	} else {
		err = a.metricFilter.RebuildDynamicLists(a.dynamicScrapper, services, a.threshold.GetThresholdMetricNames())

		if err != nil {
			logger.V(2).Printf("An error occurred while rebuilding dynamic list for updateThresholds: %v", err)
		}
	}

	for _, name := range []string{"system_pending_updates", "system_pending_security_updates"} {
		key := threshold.MetricNameItem{
			Name: name,
			Item: "",
		}
		newThreshold := a.threshold.GetThreshold(key)

		if !firstUpdate && !oldThresholds[key.Name].Equal(newThreshold) {
			a.FireTrigger(false, false, true, false)
		}
	}

	key := threshold.MetricNameItem{
		Name: "time_drift",
		Item: "",
	}
	newThreshold := a.threshold.GetThreshold(key)

	if !firstUpdate && !oldThresholds[key.Name].Equal(newThreshold) && a.bleemeoConnector != nil {
		a.bleemeoConnector.UpdateInfo()
	}
}

// Run will start the agent. It will terminate when sigquit/sigterm/sigint is received.
func (a *agent) run(stopCtx context.Context, reloadCtx context.Context, wgStop *sync.WaitGroup) { //nolint:cyclop
	ctx, cancel := context.WithCancel(reloadCtx)
	defer cancel()

	a.cancel = cancel
	a.metricResolution = 10 * time.Second
	a.hostRootPath = "/"
	a.context = ctx

	if a.oldConfig.String("container.type") != "" {
		a.hostRootPath = a.oldConfig.String("df.host_mount_point")
		setupContainer(a.hostRootPath)
	}

	a.triggerHandler = debouncer.New(
		ctx,
		a.handleTrigger,
		5*time.Second,
		10*time.Second,
	)
	a.factProvider = facts.NewFacter(
		a.oldConfig.String("agent.facts_file"),
		a.hostRootPath,
		a.oldConfig.String("agent.public_ip_indicator"),
	)

	factsMap, err := a.factProvider.FastFacts(ctx)
	if err != nil {
		logger.Printf("Warning: get facts failed, some information (e.g. name of this server) may be wrong. %v", err)
	}

	fqdn := factsMap["fqdn"]
	if fqdn == "" {
		fqdn = "localhost"
	}

	cloudImageFile := a.oldConfig.String("agent.cloudimage_creation_file")

	content, err := ioutil.ReadFile(cloudImageFile)
	if err != nil && !os.IsNotExist(err) {
		logger.Printf("Unable to read content of %#v file: %v", cloudImageFile, err)
	}

	if err == nil || !os.IsNotExist(err) {
		initialMac := parseIPOutput(content)
		currentMac := factsMap["primary_mac_address"]

		if currentMac == initialMac || currentMac == "" || initialMac == "" {
			logger.Printf("Not starting Glouton since installation for creation of a cloud image was requested and agent is still running on the same machine")
			logger.Printf("If this is wrong and agent should run on this machine, remove %#v file", cloudImageFile)

			return
		}
	}

	_ = os.Remove(cloudImageFile)

	logger.Printf("Starting agent version %v (commit %v)", version.Version, version.BuildHash)

	_ = os.Remove(a.oldConfig.String("agent.upgrade_file"))

	a.metricFormat = types.StringToMetricFormat(a.oldConfig.String("agent.metrics_format"))
	if a.metricFormat == types.MetricFormatUnknown {
		logger.Printf("Invalid metric format %#v. Supported option are \"Bleemeo\" and \"Prometheus\". Falling back to Bleemeo", a.oldConfig.String("agent.metrics_format"))
		a.metricFormat = types.MetricFormatBleemeo
	}

	apiBindAddress := fmt.Sprintf("%s:%d", a.oldConfig.String("web.listener.address"), a.oldConfig.Int("web.listener.port"))

	if a.oldConfig.Bool("agent.http_debug.enable") {
		go func() {
			debugAddress := a.oldConfig.String("agent.http_debug.bind_address")

			logger.Printf("Starting debug server on http://%s/debug/pprof/", debugAddress)
			log.Println(http.ListenAndServe(debugAddress, nil))
		}()
	}

	var targets []*scrapper.Target

	a.snmpManager = snmp.NewManager(
		a.config.SNMP.ExporterURL,
		a.factProvider,
		a.config.SNMP.Targets.ToTargetOptions()...,
	)

	if promCfg, found := a.oldConfig.Get("metric.prometheus.targets"); found {
		targets = prometheusConfigToURLs(promCfg)
	}

	mFilter, err := newMetricFilter(a.oldConfig, len(a.snmpManager.Targets()) > 0, a.metricFormat)
	if err != nil {
		logger.Printf("An error occurred while building the metric filter, allow/deny list may be partial: %v", err)
	}

	a.metricFilter = mFilter

	if a.oldConfig.Bool("web.local_ui.enable") {
		a.store = store.New(time.Hour)
	} else {
		a.store = store.New(2 * time.Minute)
	}

	filteredStore := store.NewFilteredStore(a.store, mFilter.FilterPoints, mFilter.filterMetrics)

	a.gathererRegistry, err = registry.New(
		registry.Option{
			PushPoint:             a.store,
			FQDN:                  fqdn,
			GloutonPort:           strconv.FormatInt(int64(a.oldConfig.Int("web.listener.port")), 10),
			MetricFormat:          a.metricFormat,
			BlackboxSentScraperID: a.oldConfig.Bool("blackbox.scraper_send_uuid"),
			Filter:                mFilter,
			Queryable:             a.store,
		})
	if err != nil {
		logger.Printf("Unable to create the metrics registry: %v", err)
		logger.Printf("The metrics registry is required for Glouton. Exiting.")

		return
	}

	rulesManager := rules.NewManager(ctx, a.store, a.gathererRegistry.Appendable(5*time.Minute))

	_, err = a.gathererRegistry.RegisterPushPointsCallback(
		registry.RegistrationOption{
			Description: "rulesManager",
			JitterSeed:  baseJitterPlus,
		},
		func(_ context.Context, t0 time.Time) {
			rulesManager.Run(t0)
		},
	)
	if err != nil {
		logger.Printf("unable to add recording rules metrics: %v", err)
	}

	a.threshold = threshold.New(a.state)
	acc := &inputs.Accumulator{
		Pusher:  a.threshold.WithPusher(a.gathererRegistry.WithTTL(5 * time.Minute)),
		Context: ctx,
	}

	a.dockerRuntime = &dockerRuntime.Docker{
		DockerSockets:             dockerRuntime.DefaultAddresses(a.hostRootPath),
		DeletedContainersCallback: a.deletedContainersCallback,
	}
	a.containerdRuntime = &containerd.Containerd{
		Addresses:                 containerd.DefaultAddresses(a.hostRootPath),
		DeletedContainersCallback: a.deletedContainersCallback,
	}
	a.containerRuntime = &merge.Runtime{
		Runtimes: []crTypes.RuntimeInterface{
			a.dockerRuntime,
			a.containerdRuntime,
		},
	}

	if a.oldConfig.Bool("kubernetes.enable") {
		kube := &kubernetes.Kubernetes{
			Runtime:    a.containerRuntime,
			NodeName:   a.oldConfig.String("kubernetes.nodename"),
			KubeConfig: a.oldConfig.String("kubernetes.kubeconfig"),
		}
		a.containerRuntime = kube

		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		if err := kube.Test(ctx); err != nil {
			logger.Printf("Kubernetes API unreachable, service detection may misbehave: %v", err)
		}

		cancel()
	}

	var psLister facts.ProcessLister

	useProc := a.oldConfig.String("container.type") == "" || a.oldConfig.Bool("container.pid_namespace_host")
	if !useProc {
		logger.V(1).Printf("The agent is running in a container and \"container.pid_namespace_host\", is not true. Not all processes will be seen")
	} else {
		if !version.IsLinux() {
			psLister = facts.NewPsUtilLister("")
		} else {
			psLister = process.NewProcessLister(a.hostRootPath, 9*time.Second)
		}
	}

	psFact := facts.NewProcess(
		psLister,
		a.hostRootPath,
		a.containerRuntime,
	)
	netstat := &facts.NetstatProvider{FilePath: a.oldConfig.String("agent.netstat_file")}

	a.factProvider.AddCallback(a.containerRuntime.RuntimeFact)
	a.factProvider.SetFact("installation_format", a.oldConfig.String("agent.installation_format"))

	processInput := processInput.New(psFact, a.threshold.WithPusher(a.gathererRegistry.WithTTL(5*time.Minute)))

	a.collector = collector.New(acc)

	_, err = a.gathererRegistry.RegisterPushPointsCallback(
		registry.RegistrationOption{
			Description: "system & services metrics",
			JitterSeed:  baseJitter,
		},
		a.collector.RunGather,
	)
	if err != nil {
		logger.Printf("unable to add system metrics: %v", err)
	}

	if a.metricFormat == types.MetricFormatBleemeo {
		_, err = a.gathererRegistry.RegisterPushPointsCallback(
			registry.RegistrationOption{
				Description: "procces status metrics",
				JitterSeed:  baseJitter,
			},
			processInput.Gather,
		)
		if err != nil {
			logger.Printf("unable to add processes metrics: %v", err)
		}
	}

	_, err = a.gathererRegistry.RegisterPushPointsCallback(
		registry.RegistrationOption{
			Description: "miscGather",
			JitterSeed:  baseJitter,
		},
		a.miscGather(a.threshold.WithPusher(a.gathererRegistry.WithTTL(5*time.Minute))),
	)
	if err != nil {
		logger.Printf("unable to add miscGathere metrics: %v", err)
	}

	servicesIgnoreCheck, _ := a.oldConfig.Get("service_ignore_check")
	servicesIgnoreMetrics, _ := a.oldConfig.Get("service_ignore_metrics")
	serviceIgnoreCheck := confFieldToSliceMap(servicesIgnoreCheck, "service ignore check")
	serviceIgnoreMetrics := confFieldToSliceMap(servicesIgnoreMetrics, "service ignore metrics")
	isCheckIgnored := discovery.NewIgnoredService(serviceIgnoreCheck).IsServiceIgnored
	isInputIgnored := discovery.NewIgnoredService(serviceIgnoreMetrics).IsServiceIgnored
	dynamicDiscovery := discovery.NewDynamic(psFact, netstat, a.containerRuntime, discovery.SudoFileReader{HostRootPath: a.hostRootPath}, a.oldConfig.String("stack"))
	a.discovery = discovery.New(
		dynamicDiscovery,
		a.collector,
		a.gathererRegistry,
		a.taskRegistry,
		a.state,
		acc,
		a.containerRuntime,
		a.config.Services.ToDiscoveryMap(),
		isCheckIgnored,
		isInputIgnored,
		a.metricFormat,
	)

	a.updateSNMPResolution(time.Minute)

	for _, target := range targets {
		hash := labels.FromMap(target.ExtraLabels).Hash()

		_, err = a.gathererRegistry.RegisterGatherer(
			registry.RegistrationOption{
				Description: "Prom exporter " + target.URL.String(),
				JitterSeed:  hash,
				Interval:    defaultInterval,
				ExtraLabels: target.ExtraLabels,
			},
			target,
			true,
		)
		if err != nil {
			logger.Printf("Unable to add Prometheus scrapper for target %s: %v", target.URL.String(), err)
		}
	}

	a.gathererRegistry.AddDefaultCollector()

	a.dynamicScrapper = &promexporter.DynamicScrapper{
		Registry:       a.gathererRegistry,
		DynamicJobName: "discovered-exporters",
	}

	if _, found := a.oldConfig.Get("metric.pull"); found {
		logger.Printf("metric.pull is deprecated and not supported by Glouton.")
		logger.Printf("For your custom metrics, please use Prometheus exporter & metric.prometheus")
	}

	if a.oldConfig.Bool("blackbox.enable") {
		logger.V(1).Println("Starting blackbox_exporter...")
		// the config is present, otherwise we would not be in this block
		blackboxConf, _ := a.oldConfig.Get("blackbox")

		a.monitorManager, err = blackbox.New(a.gathererRegistry, blackboxConf, a.oldConfig.String("blackbox.user_agent"), a.metricFormat)
		if err != nil {
			logger.V(0).Printf("Couldn't start blackbox_exporter: %v\nMonitors will not be able to run on this agent.", err)
		}
	} else {
		logger.V(1).Println("blackbox_exporter not enabled, will not start...")
	}

	promExporter := a.gathererRegistry.Exporter()

	if a.oldConfig.Bool("agent.process_exporter.enable") {
		process.RegisterExporter(a.gathererRegistry, psLister, dynamicDiscovery, a.metricFormat == types.MetricFormatBleemeo)
	}

	api := &api.API{
		DB:                 a.store,
		ContainerRuntime:   a.containerRuntime,
		PsFact:             psFact,
		FactProvider:       a.factProvider,
		BindAddress:        apiBindAddress,
		Disccovery:         a.discovery,
		AgentInfo:          a,
		PrometheurExporter: promExporter,
		Threshold:          a.threshold,
		StaticCDNURL:       a.oldConfig.String("web.static_cdn_url"),
		DiagnosticPage:     a.DiagnosticPage,
		DiagnosticArchive:  a.writeDiagnosticArchive,
		MetricFormat:       a.metricFormat,
		LocalUIDisabled:    !a.oldConfig.Bool("web.local_ui.enable"),
	}

	a.FireTrigger(true, true, false, false)

	tasks := []taskInfo{
		{a.watchdog, "Agent Watchdog"},
		{a.store.Run, "Metric store"},
		{a.containerRuntime.Run, "Docker connector"},
		{a.healthCheck, "Agent healthcheck"},
		{a.hourlyDiscovery, "Service Discovery"},
		{a.dailyFact, "Facts gatherer"},
		{a.dockerWatcher, "Docker event watcher"},
		{a.netstatWatcher, "Netstat file watcher"},
		{a.miscTasks, "Miscelanous tasks"},
		{a.minuteMetric, "Metrics every minute"},
		{a.sendToTelemetry, "Send Facts information to our telemetry tool"},
	}

	if a.oldConfig.Bool("web.enable") {
		tasks = append(tasks, taskInfo{api.Run, "Local Web UI"})
	}

	if a.oldConfig.Bool("jmx.enable") {
		perm, err := strconv.ParseInt(a.oldConfig.String("jmxtrans.file_permission"), 8, 0)
		if err != nil {
			logger.Printf("invalid permission %#v: %v", a.oldConfig.String("jmxtrans.file_permission"), err)
			logger.Printf("using the default 0640")

			perm = 0o640
		}

		a.jmx = &jmxtrans.JMX{
			OutputConfigurationFile:       a.oldConfig.String("jmxtrans.config_file"),
			OutputConfigurationPermission: os.FileMode(perm),
			ContactPort:                   a.oldConfig.Int("jmxtrans.graphite_port"),
			Pusher:                        a.threshold.WithPusher(a.gathererRegistry.WithTTL(5 * time.Minute)),
		}

		tasks = append(tasks, taskInfo{a.jmx.Run, "jmxtrans"})
	}

	if a.oldConfig.Bool("bleemeo.enable") {
		scaperName := a.oldConfig.String("blackbox.scraper_name")
		if scaperName == "" {
			scaperName = fmt.Sprintf("%s:%d", fqdn, a.oldConfig.Int("web.listener.port"))
		}

		a.bleemeoConnector, err = bleemeo.New(bleemeoTypes.GlobalOption{
			Config:                  a.oldConfig,
			State:                   a.state,
			Facts:                   a.factProvider,
			Process:                 psFact,
			Docker:                  a.containerRuntime,
			Store:                   filteredStore,
			SNMP:                    a.snmpManager.Targets(),
			SNMPOnlineTarget:        a.snmpManager.OnlineCount,
			Acc:                     acc,
			Discovery:               a.discovery,
			MonitorManager:          a.monitorManager,
			UpdateMetricResolution:  a.updateMetricResolution,
			UpdateThresholds:        a.UpdateThresholds,
			UpdateUnits:             a.threshold.SetUnits,
			MetricFormat:            a.metricFormat,
			NotifyFirstRegistration: a.notifyBleemeoFirstRegistration,
			NotifyLabelsUpdate:      a.notifyBleemeoUpdateLabels,
			BlackboxScraperName:     scaperName,
		}, stopCtx, wgStop)
		if err != nil {
			logger.Printf("unable to start Bleemeo SAAS connector: %v", err)

			return
		}

		a.gathererRegistry.UpdateRelabelHook(ctx, a.bleemeoConnector.RelabelHook)
		tasks = append(tasks, taskInfo{a.bleemeoConnector.Run, "Bleemeo SAAS connector"})
	}

	sentry.ConfigureScope(func(scope *sentry.Scope) {
		scope.SetContext("agent", map[string]interface{}{
			"agent_id":        a.BleemeoAgentID(),
			"glouton_version": version.Version,
		})
	})

	if a.oldConfig.Bool("nrpe.enable") {
		nrpeConfFile := a.oldConfig.StringList("nrpe.conf_paths")
		nrperesponse := nrpe.NewResponse(a.config.Services.ToNRPEMap(), a.discovery, nrpeConfFile)
		server := nrpe.New(
			fmt.Sprintf("%s:%d", a.oldConfig.String("nrpe.address"), a.oldConfig.Int("nrpe.port")),
			a.oldConfig.Bool("nrpe.ssl"),
			nrperesponse.Response,
		)
		tasks = append(tasks, taskInfo{server.Run, "NRPE server"})
	}

	if a.oldConfig.Bool("zabbix.enable") {
		server := zabbix.New(
			fmt.Sprintf("%s:%d", a.oldConfig.String("zabbix.address"), a.oldConfig.Int("zabbix.port")),
			zabbixResponse,
		)
		tasks = append(tasks, taskInfo{server.Run, "Zabbix server"})
	}

	if a.oldConfig.Bool("influxdb.enable") {
		server := influxdb.New(
			fmt.Sprintf("http://%s:%s", a.oldConfig.String("influxdb.host"), a.oldConfig.String("influxdb.port")),
			a.oldConfig.String("influxdb.db_name"),
			a.store,
			a.oldConfig.StringMap("influxdb.tags"),
		)
		a.influxdbConnector = server
		tasks = append(tasks, taskInfo{server.Run, "influxdb"})

		logger.V(2).Printf("Influxdb is activated !")
	}

	if a.bleemeoConnector == nil {
		a.updateThresholds(nil, true)
	} else {
		a.bleemeoConnector.ApplyCachedConfiguration()
	}

	tmp, _ := a.oldConfig.Get("metric.softstatus_period")

	a.threshold.SetSoftPeriod(
		time.Duration(a.oldConfig.Int("metric.softstatus_period_default"))*time.Second,
		softPeriodsFromInterface(tmp),
	)

	if !reflect.DeepEqual(a.oldConfig.StringList("disk_monitor"), defaultConfig()["disk_monitor"]) {
		if a.metricFormat == types.MetricFormatBleemeo && len(a.oldConfig.StringList("disk_ignore")) > 0 {
			logger.Printf("Warning: both \"disk_monitor\" and \"disk_ignore\" are set. Only \"disk_ignore\" will be used")
		} else if a.metricFormat != types.MetricFormatBleemeo {
			logger.Printf("Warning: configuration \"disk_monitor\" is not used in Prometheus mode. Use \"disk_ignore\"")
		}
	}

	if a.metricFormat == types.MetricFormatBleemeo {
		conf, err := a.buildCollectorsConfig()
		if err != nil {
			logger.V(0).Printf("Unable to initialize system collector: %v", err)

			return
		}

		if err = discovery.AddDefaultInputs(a.collector, conf); err != nil {
			logger.Printf("Unable to initialize system collector: %v", err)

			return
		}
	}

	// register components only available on a given system, like node_exporter for unixes
	a.registerOSSpecificComponents()

	tasks = append(tasks, taskInfo{
		a.gathererRegistry.Run,
		"Metric collector",
	})

	if a.oldConfig.Bool("telegraf.statsd.enable") {
		input, err := statsd.New(fmt.Sprintf("%s:%d", a.oldConfig.String("telegraf.statsd.address"), a.oldConfig.Int("telegraf.statsd.port")))
		if err != nil {
			logger.Printf("Unable to create StatsD input: %v", err)
			a.oldConfig.Set("telegraf.statsd.enable", false)
		} else if _, err = a.collector.AddInput(input, "statsd"); err != nil {
			if strings.Contains(err.Error(), "address already in use") {
				logger.Printf("Unable to listen on StatsD port because another program already use it")
				logger.Printf("The StatsD integration is now disabled. Restart the agent to try re-enabling it.")
				logger.Printf("See https://docs.bleemeo.com/agent/configuration#telegrafstatsdenable to permanently disable StatsD integration or using an alternate port")
			} else {
				logger.Printf("Unable to create StatsD input: %v", err)
			}

			a.oldConfig.Set("telegraf.statsd.enable", false)
		}
	}

	a.factProvider.SetFact("statsd_enable", a.oldConfig.String("telegraf.statsd.enable"))
	a.factProvider.SetFact("metrics_format", a.metricFormat.String())

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)

	go a.handleSignals(ctx, c, cancel)

	a.startTasks(tasks)

	<-ctx.Done()
	logger.V(2).Printf("Stopping agent...")
	signal.Stop(c)
	close(c)
	a.taskRegistry.Close()
	a.discovery.Close()
	a.collector.Close()
	logger.V(2).Printf("Agent stopped")
}

func (a *agent) handleSignals(ctx context.Context, c chan os.Signal, cancel context.CancelFunc) {
	var (
		l                         sync.Mutex
		systemUpdateMetricPending bool
	)

	for s := range c {
		if s == syscall.SIGTERM || s == syscall.SIGINT || s == os.Interrupt {
			cancel()

			break
		}

		if s == syscall.SIGHUP {
			if a.bleemeoConnector != nil {
				a.bleemeoConnector.UpdateMonitors()
			}

			l.Lock()
			if !systemUpdateMetricPending {
				systemUpdateMetricPending = true

				go func() {
					t0 := time.Now()

					for n := 0; n < 10; n++ {
						time.Sleep(time.Second)

						updatedAt := facts.PendingSystemUpdateFreshness(
							ctx,
							a.oldConfig.String("container.type") != "",
							a.hostRootPath,
						)
						if updatedAt.IsZero() || updatedAt.After(t0) {
							break
						}
					}

					a.FireTrigger(false, false, true, false)

					l.Lock()

					systemUpdateMetricPending = false

					l.Unlock()
				}()
			}
			l.Unlock()

			a.FireTrigger(true, true, false, true)
		}
	}
}

func (a *agent) buildCollectorsConfig() (conf inputs.CollectorConfig, err error) {
	whitelistRE, err := common.CompileREs(a.oldConfig.StringList("disk_monitor"))
	if err != nil {
		logger.V(1).Printf("the whitelist for diskio regexp couldn't compile: %s", err)

		return
	}

	blacklistRE, err := common.CompileREs(a.oldConfig.StringList("disk_ignore"))
	if err != nil {
		logger.V(1).Printf("the blacklist for diskio regexp couldn't compile: %s", err)

		return
	}

	pathBlacklist := a.oldConfig.StringList("df.path_ignore")
	pathBlacklistTrimed := make([]string, len(pathBlacklist))

	for i, v := range pathBlacklist {
		pathBlacklistTrimed[i] = strings.TrimRight(v, "/")
	}

	return inputs.CollectorConfig{
		DFRootPath:      a.hostRootPath,
		NetIfBlacklist:  a.oldConfig.StringList("network_interface_blacklist"),
		IODiskWhitelist: whitelistRE,
		IODiskBlacklist: blacklistRE,
		DFPathBlacklist: pathBlacklistTrimed,
	}, nil
}

func (a *agent) miscGather(pusher types.PointPusher) func(context.Context, time.Time) {
	return func(ctx context.Context, t0 time.Time) {
		points, err := a.containerRuntime.Metrics(ctx)
		if err != nil {
			logger.V(2).Printf("container Runtime metrics gather failed: %v", err)
		}

		// We don't really care about having up-to-date information because
		// when containers are started/stopped, the information is updated anyway.
		containers, err := a.containerRuntime.Containers(ctx, 2*time.Hour, false)
		if err != nil {
			logger.V(2).Printf("gather on DockerProvider failed: %v", err)

			return
		}

		countRunning := 0

		for _, c := range containers {
			if c.State().IsRunning() {
				countRunning++
			}
		}

		points = append(points, types.MetricPoint{
			Point: types.Point{Time: t0, Value: float64(countRunning)},
			Labels: map[string]string{
				"__name__": "containers_count",
			},
		})

		pusher.PushPoints(ctx, points)
	}
}

func (a *agent) sendToTelemetry(ctx context.Context) error {
	if a.oldConfig.Bool("agent.telemetry.enable") {
		select {
		case <-time.After(delay.JitterDelay(5*time.Minute, 0.2)):
		case <-ctx.Done():
			return nil
		}

		for {
			facts, err := a.factProvider.Facts(ctx, time.Hour)
			if err != nil {
				logger.V(2).Printf("error facts load %v", err)

				continue
			}

			tlm := telemetry.FromState(a.state)

			if tlm.ID == "" {
				var t telemetry.Telemetry
				t.ID = uuid.New().String()
				t.SaveState(a.state)
				tlm = t
			}

			tlm.PostInformation(ctx, a.oldConfig.String("agent.telemetry.address"), a.BleemeoAgentID(), facts)

			select {
			case <-time.After(delay.JitterDelay(24*time.Hour, 0.05)):
			case <-ctx.Done():
				return nil
			}
		}
	}

	return nil
}

func (a *agent) minuteMetric(ctx context.Context) error {
	for {
		select {
		case <-time.After(time.Minute):
		case <-ctx.Done():
			return nil
		}

		service, err := a.discovery.Discovery(ctx, 2*time.Hour)
		if err != nil {
			logger.V(1).Printf("get service failed to every-minute metrics: %v", err)

			continue
		}

		for _, srv := range service {
			if !srv.Active {
				continue
			}

			switch srv.ServiceType { //nolint:exhaustive,nolintlint
			case discovery.PostfixService:
				n, err := postfixQueueSize(ctx, srv, a.hostRootPath, a.containerRuntime)
				if err != nil {
					logger.V(1).Printf("Unabled to gather postfix queue size on %s: %v", srv, err)

					continue
				}

				labels := map[string]string{
					types.LabelName:              "postfix_queue_size",
					types.LabelMetaContainerName: srv.ContainerName,
					types.LabelMetaContainerID:   srv.ContainerID,
					types.LabelMetaServiceName:   srv.ContainerName,
				}

				annotations := types.MetricAnnotations{
					BleemeoItem: srv.ContainerName,
					ContainerID: srv.ContainerID,
					ServiceName: srv.Name,
				}

				a.threshold.WithPusher(a.gathererRegistry.WithTTL(5*time.Minute)).PushPoints(ctx, []types.MetricPoint{
					{
						Labels:      labels,
						Annotations: annotations,
						Point: types.Point{
							Time:  time.Now(),
							Value: n,
						},
					},
				})
			case discovery.EximService:
				n, err := eximQueueSize(ctx, srv, a.hostRootPath, a.containerRuntime)
				if err != nil {
					logger.V(1).Printf("Unabled to gather exim queue size on %s: %v", srv, err)

					continue
				}

				labels := map[string]string{
					types.LabelName:              "exim_queue_size",
					types.LabelMetaContainerName: srv.ContainerName,
					types.LabelMetaContainerID:   srv.ContainerID,
					types.LabelMetaServiceName:   srv.ContainerName,
				}

				annotations := types.MetricAnnotations{
					BleemeoItem: srv.ContainerName,
					ContainerID: srv.ContainerID,
					ServiceName: srv.Name,
				}

				a.threshold.WithPusher(a.gathererRegistry.WithTTL(5*time.Minute)).PushPoints(ctx, []types.MetricPoint{
					{
						Labels:      labels,
						Annotations: annotations,
						Point: types.Point{
							Time:  time.Now(),
							Value: n,
						},
					},
				})
			}
		}

		desc := strings.Join(a.oldConfig.GetWarnings(), "\n")
		status := types.StatusWarning
		t0 := time.Now().Truncate(time.Second)

		if len(desc) == 0 {
			status = types.StatusOk
			desc = "configuration returned no warnings."
		}

		a.gathererRegistry.WithTTL(5*time.Minute).PushPoints(ctx, []types.MetricPoint{
			{
				Point: types.Point{
					Value: float64(status.NagiosCode()),
					Time:  t0,
				},
				Labels: map[string]string{
					types.LabelName: "agent_config_warning",
				},
				Annotations: types.MetricAnnotations{
					Status: types.StatusDescription{
						StatusDescription: desc,
						CurrentStatus:     status,
					},
				},
			},
		})
	}
}

func (a *agent) miscTasks(ctx context.Context) error {
	lastTime := time.Now()

	for {
		select {
		case <-time.After(30 * time.Second):
		case <-ctx.Done():
			return nil
		}

		now := time.Now()

		jump := math.Abs(30 - float64(now.Unix()-lastTime.Unix()))
		if jump > 60 {
			// It looks like time jumped. This could be either:
			// * suspending
			// * or time changed (ntp fixed the time ?)
			// Trigger a UpdateInfo to check time_drift
			if a.bleemeoConnector != nil {
				a.bleemeoConnector.UpdateInfo()
			}
		}

		lastTime = now

		a.triggerLock.Lock()
		if !a.triggerDiscAt.IsZero() && time.Now().After(a.triggerDiscAt) {
			a.triggerDiscAt = time.Time{}
			a.triggerDiscImmediate = true
			a.triggerHandler.Trigger()
		}
		a.triggerLock.Unlock()
	}
}

func (a *agent) startTasks(tasks []taskInfo) {
	a.l.Lock()
	defer a.l.Unlock()

	for _, t := range tasks {
		id, err := a.taskRegistry.AddTask(t.function, t.name)
		if err != nil {
			logger.V(1).Printf("Unable to start %s: %v", t.name, err)
		}

		a.taskIDs[t.name] = id
	}
}

func (a *agent) watchdog(ctx context.Context) error {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	failing := false

	for {
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return nil
		}

		now := time.Now()

		a.l.Lock()

		lastHealCheck := a.lastHealCheck
		a.watchdogRunAt = append(a.watchdogRunAt, now)

		if len(a.watchdogRunAt) > 90 {
			copy(a.watchdogRunAt[0:60], a.watchdogRunAt[len(a.watchdogRunAt)-60:len(a.watchdogRunAt)])
			a.watchdogRunAt = a.watchdogRunAt[:60]
		}

		a.l.Unlock()

		switch {
		case time.Since(lastHealCheck) > 15*time.Minute && !failing:
			logger.V(2).Printf("Healcheck are no longer running. Last run was at %s", lastHealCheck.Format(time.RFC3339))

			failing = true
		case time.Since(lastHealCheck) > 15*time.Minute && failing:
			logger.Printf("Healcheck are no longer running. Last run was at %s", lastHealCheck.Format(time.RFC3339))
			// We don't know how big the buffer needs to be to collect
			// all the goroutines. Use 2MB buffer which hopefully is enough
			buffer := make([]byte, 1<<21)

			runtime.Stack(buffer, true)
			logger.Printf("%s", string(buffer))
			logger.Printf("Glouton seems unhealthy, killing myself")
			panic("Glouton seems unhealthy, killing myself")
		default:
			failing = false
		}
	}
}

func (a *agent) healthCheck(ctx context.Context) error {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return nil
		}

		mandatoryTasks := []string{"Bleemeo SAAS connector", "Metric collector", "Metric store"}
		for _, name := range mandatoryTasks {
			if crashed, err := a.doesTaskCrashed(ctx, name); crashed && err != nil {
				logger.Printf("Task %#v crashed: %v", name, err)
				logger.Printf("Stopping the agent as task %#v is critical", name)
				a.cancel()
			}
		}

		if a.bleemeoConnector != nil {
			a.bleemeoConnector.HealthCheck()
		}

		if a.influxdbConnector != nil {
			a.influxdbConnector.HealthCheck()
		}

		a.l.Lock()
		a.lastHealCheck = time.Now()
		a.l.Unlock()
	}
}

// Return true if the given task exited before ctx was terminated
// Also return the error the tasks returned.
func (a *agent) doesTaskCrashed(ctx context.Context, name string) (bool, error) {
	a.l.Lock()
	defer a.l.Unlock()

	if id, ok := a.taskIDs[name]; ok {
		running, err := a.taskRegistry.IsRunning(id)
		if !running {
			// Re-check ctx to avoid race condition, it crashed only if we are still running
			return ctx.Err() == nil, err
		}
	}

	return false, nil
}

func (a *agent) hourlyDiscovery(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return nil
	case <-time.After(15 * time.Second):
	}

	a.FireTrigger(false, false, true, false)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(delay.JitterDelay(time.Hour, 0.1)):
			a.FireTrigger(true, false, true, false)
		}
	}
}

func (a *agent) dailyFact(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(delay.JitterDelay(24*time.Hour, 0.1)):
			a.FireTrigger(false, true, false, false)
		}
	}
}

func (a *agent) dockerWatcher(ctx context.Context) error {
	var wg sync.WaitGroup

	wg.Add(1)

	go func() {
		defer wg.Done()
		a.dockerWatcherContainerHealth(ctx)
	}()

	defer wg.Wait()

	pendingTimer := time.NewTimer(0 * time.Second)
	// drain (expire) the timer, so the invariant "pendingTimer is expired when pendingDiscovery == false" hold.
	<-pendingTimer.C

	pendingDiscovery := false
	pendingSecondDiscovery := false

	for {
		select {
		case ev := <-a.containerRuntime.Events():
			a.l.Lock()
			a.lastContainerEventTime = time.Now()
			a.l.Unlock()

			if ev.Type == facts.EventTypeStart {
				pendingSecondDiscovery = true
			}

			if !pendingDiscovery && (ev.Type == facts.EventTypeStart || ev.Type == facts.EventTypeStop || ev.Type == facts.EventTypeDelete) {
				pendingDiscovery = true

				pendingTimer.Reset(5 * time.Second)
			}

			if ev.Type == facts.EventTypeHealth && ev.Container != nil {
				if a.bleemeoConnector != nil {
					a.bleemeoConnector.UpdateContainers()
				}

				a.sendDockerContainerHealth(ctx, ev.Container)
			}
		case <-pendingTimer.C:
			if pendingDiscovery {
				a.FireTrigger(pendingDiscovery, false, false, pendingSecondDiscovery)
				pendingDiscovery = false
				pendingSecondDiscovery = false
			}
		case <-ctx.Done():
			return nil
		}
	}
}

func (a *agent) dockerWatcherContainerHealth(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// It not needed to have fresh container information. When health event occur,
			// DockerFact already update the container information
			containers, err := a.containerRuntime.Containers(ctx, 3600*time.Second, false)
			if err != nil {
				continue
			}

			for _, c := range containers {
				a.sendDockerContainerHealth(ctx, c)
			}
		case <-ctx.Done():
			return
		}
	}
}

func (a *agent) sendDockerContainerHealth(ctx context.Context, container facts.Container) {
	health, message := container.Health()
	if health == facts.ContainerNoHealthCheck {
		return
	}

	status := types.StatusDescription{}

	switch {
	case !container.State().IsRunning():
		status.CurrentStatus = types.StatusCritical
		status.StatusDescription = "Container stopped"
	case health == facts.ContainerHealthy:
		status.CurrentStatus = types.StatusOk
		status.StatusDescription = message
	case health == facts.ContainerStarting:
		startedAt := container.StartedAt()
		if time.Since(startedAt) < time.Minute || startedAt.IsZero() {
			status.CurrentStatus = types.StatusOk
		} else {
			status.CurrentStatus = types.StatusWarning
			status.StatusDescription = "Container is still starting"
		}
	case health == facts.ContainerUnhealthy:
		status.CurrentStatus = types.StatusCritical
		status.StatusDescription = message
	default:
		status.CurrentStatus = types.StatusUnknown
		status.StatusDescription = fmt.Sprintf("Unknown health status %s", message)
	}

	a.gathererRegistry.WithTTL(5*time.Minute).PushPoints(ctx, []types.MetricPoint{
		{
			Labels: map[string]string{
				types.LabelName:              "container_health_status",
				types.LabelMetaContainerName: container.ContainerName(),
			},
			Annotations: types.MetricAnnotations{
				Status:      status,
				ContainerID: container.ID(),
				BleemeoItem: container.ContainerName(),
			},
			Point: types.Point{
				Time:  time.Now(),
				Value: float64(status.CurrentStatus.NagiosCode()),
			},
		},
	})
}

func (a *agent) netstatWatcher(ctx context.Context) error {
	filePath := a.oldConfig.String("agent.netstat_file")
	stat, _ := os.Stat(filePath)

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}

		newStat, _ := os.Stat(filePath)
		if newStat != nil && (stat == nil || !newStat.ModTime().Equal(stat.ModTime())) {
			a.FireTrigger(true, false, false, false)
		}

		stat = newStat
	}
}

func (a *agent) FireTrigger(discovery bool, sendFacts bool, systemUpdateMetric bool, secondDiscovery bool) {
	a.triggerLock.Lock()
	defer a.triggerLock.Unlock()

	if discovery {
		a.triggerDiscImmediate = true
	}

	if sendFacts {
		a.triggerFact = true
	}

	if systemUpdateMetric {
		a.triggerSystemUpdateMetric = true
	}

	// Some discovery request ask for a second discovery in 1 minutes.
	// The second discovery allow to discovery service that are slow to start
	if secondDiscovery {
		deadline := time.Now().Add(time.Minute)
		a.triggerDiscAt = deadline
	}

	a.triggerHandler.Trigger()
}

func (a *agent) cleanTrigger() (discovery bool, sendFacts bool, systemUpdateMetric bool) {
	a.triggerLock.Lock()
	defer a.triggerLock.Unlock()

	discovery = a.triggerDiscImmediate
	sendFacts = a.triggerFact
	systemUpdateMetric = a.triggerSystemUpdateMetric
	a.triggerSystemUpdateMetric = false
	a.triggerDiscImmediate = false
	a.triggerFact = false

	return
}

//nolint:cyclop
func (a *agent) handleTrigger(ctx context.Context) {
	runDiscovery, runFact, runSystemUpdateMetric := a.cleanTrigger()
	if runDiscovery {
		services, err := a.discovery.Discovery(ctx, 0)
		if err != nil {
			logger.V(1).Printf("error during discovery: %v", err)
		} else {
			if a.jmx != nil {
				a.l.Lock()
				resolution := a.metricResolution
				a.l.Unlock()

				if err := a.jmx.UpdateConfig(services, resolution); err != nil {
					logger.V(1).Printf("failed to update JMX configuration: %v", err)
				}
			}
			if a.dynamicScrapper != nil {
				if containers, err := a.containerRuntime.Containers(ctx, time.Hour, false); err == nil {
					a.dynamicScrapper.Update(containers)
				}
			}

			err := a.metricFilter.RebuildDynamicLists(a.dynamicScrapper, services, a.threshold.GetThresholdMetricNames())
			if err != nil {
				logger.V(2).Printf("Error during dynamic Filter rebuild: %v", err)
			}
		}

		hasConnection := a.dockerRuntime.IsRuntimeRunning(ctx)
		if hasConnection && !a.dockerInputPresent && a.oldConfig.Bool("telegraf.docker_metrics_enable") {
			i, err := docker.New(a.dockerRuntime.ServerAddress(), a.dockerRuntime)
			if err != nil {
				logger.V(1).Printf("error when creating Docker input: %v", err)
			} else {
				logger.V(2).Printf("Enable Docker metrics")
				a.dockerInputID, _ = a.collector.AddInput(i, "docker")
				a.dockerInputPresent = true
			}
		} else if !hasConnection && a.dockerInputPresent {
			logger.V(2).Printf("Disable Docker metrics")
			a.collector.RemoveInput(a.dockerInputID)
			a.dockerInputPresent = false
		}
	}

	if runFact {
		if _, err := a.factProvider.Facts(ctx, 0); err != nil {
			logger.V(1).Printf("error during facts gathering: %v", err)
		}
	}

	if runSystemUpdateMetric {
		systemUpdateMetric(ctx, a)
	}
}

func systemUpdateMetric(ctx context.Context, a *agent) {
	pendingUpdate, pendingSecurityUpdate := facts.PendingSystemUpdate(
		ctx,
		a.oldConfig.String("container.type") != "",
		a.hostRootPath,
	)

	points := make([]types.MetricPoint, 0)

	if pendingUpdate >= 0 {
		points = append(points, types.MetricPoint{
			Labels: map[string]string{
				types.LabelName: "system_pending_updates",
			},
			Point: types.Point{
				Time:  time.Now(),
				Value: float64(pendingUpdate),
			},
		})
	}

	if pendingSecurityUpdate >= 0 {
		points = append(points, types.MetricPoint{
			Labels: map[string]string{
				types.LabelName: "system_pending_security_updates",
			},
			Point: types.Point{
				Time:  time.Now(),
				Value: float64(pendingSecurityUpdate),
			},
		})
	}

	a.threshold.WithPusher(a.gathererRegistry.WithTTL(time.Hour)).PushPoints(ctx, points)
}

func (a *agent) deletedContainersCallback(containersID []string) {
	metrics, err := a.store.Metrics(nil)
	if err != nil {
		logger.V(1).Printf("Unable to list metrics to cleanup after container deletion: %v", err)

		return
	}

	var metricToDelete []map[string]string

	for _, m := range metrics {
		annotations := m.Annotations()
		for _, c := range containersID {
			if annotations.ContainerID == c {
				metricToDelete = append(metricToDelete, m.Labels())
			}
		}
	}

	if len(metricToDelete) > 0 {
		a.store.DropMetrics(metricToDelete)
	}
}

// migrateState update older state to latest version.
func (a *agent) migrateState() {
	// This "secret" was only present in Bleemeo agent and not really used.
	_ = a.state.Delete("web_secret_key")
}

// DiagnosticPage return useful information to troubleshoot issue.
func (a *agent) DiagnosticPage(ctx context.Context) string {
	builder := &strings.Builder{}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	fmt.Fprintf(
		builder,
		"Run diagnostic at %s with Glouton version %s (commit %s built using Go %s)\n",
		time.Now().Format(time.RFC3339),
		version.Version,
		version.BuildHash,
		runtime.Version(),
	)

	if a.oldConfig.Bool("bleemeo.enable") {
		fmt.Fprintln(builder, "Glouton has Bleemeo connection enabled")

		if a.bleemeoConnector == nil {
			fmt.Fprintln(builder, "Unexpected error: Bleemeo is enabled by Bleemeo connector is not created")
		} else {
			builder.WriteString(a.bleemeoConnector.DiagnosticPage())
		}
	} else {
		fmt.Fprintln(builder, "Glouton has Bleemeo connection DISABLED")
	}

	allMetrics, err := a.store.Metrics(nil)
	if err != nil {
		fmt.Fprintf(builder, "Unable to query internal metrics store: %v\n", err)
	} else {
		fmt.Fprintf(builder, "Glouton measure %d metrics\n", len(allMetrics))
	}

	fmt.Fprintf(builder, "Glouton was build for %s %s\n", runtime.GOOS, runtime.GOARCH)

	facts, err := a.factProvider.Facts(ctx, time.Hour)
	if err != nil {
		fmt.Fprintf(builder, "Unable to gather facts: %v\n", err)
	} else {
		lines := make([]string, 0, len(facts))

		for k, v := range facts {
			lines = append(lines, " * "+k+" = "+v)
		}

		sort.Strings(lines)

		fmt.Fprintln(builder, "Facts:")
		for _, l := range lines {
			fmt.Fprintln(builder, l)
		}
	}

	return builder.String()
}

func (a *agent) writeDiagnosticArchive(ctx context.Context, archive types.ArchiveWriter) error {
	modules := []func(ctx context.Context, archive types.ArchiveWriter) error{
		a.diagnosticGlobalInfo,
		a.diagnosticGloutonState,
		a.diagnosticJitter,
		a.taskRegistry.DiagnosticArchive,
		a.store.DiagnosticArchive,
		a.diagnosticConfig,
		a.discovery.DiagnosticArchive,
		a.diagnosticContainers,
		a.diagnosticSNMP,
		a.metricFilter.DiagnosticArchive,
		a.gathererRegistry.DiagnosticArchive,
	}

	if a.bleemeoConnector != nil {
		modules = append(modules, a.bleemeoConnector.DiagnosticArchive)
	}

	if a.monitorManager != nil {
		modules = append(modules, a.monitorManager.DiagnosticArchive)
	}

	for _, f := range modules {
		if err := f(ctx, archive); err != nil {
			return err
		}

		if ctx.Err() != nil {
			break
		}
	}

	return ctx.Err()
}

func (a *agent) diagnosticGlobalInfo(ctx context.Context, archive types.ArchiveWriter) error {
	file, err := archive.Create("diagnostic.txt")
	if err != nil {
		return err
	}

	_, err = file.Write([]byte(a.DiagnosticPage(ctx)))
	if err != nil {
		return err
	}

	file, err = archive.Create("log.txt")
	if err != nil {
		return err
	}

	_, err = file.Write(logger.Buffer())
	if err != nil {
		return err
	}

	file, err = archive.Create("goroutines.txt")
	if err != nil {
		return err
	}

	// We don't know how big the buffer needs to be to collect
	// all the goroutines. Use 2MB buffer which hopefully is enough
	buffer := make([]byte, 1<<21)

	n := runtime.Stack(buffer, true)
	buffer = buffer[:n]

	_, err = file.Write(buffer)
	if err != nil {
		return err
	}

	return nil
}

func (a *agent) diagnosticGloutonState(ctx context.Context, archive types.ArchiveWriter) error {
	file, err := archive.Create("glouton-state.json")
	if err != nil {
		return err
	}

	a.l.Lock()
	a.triggerLock.Lock()

	obj := struct {
		HostRootPath              string
		LastHealCheck             time.Time
		LastContainerEventTime    time.Time
		TriggerDiscAt             time.Time
		TriggerDiscImmediate      bool
		TriggerFact               bool
		TriggerSystemUpdateMetric bool
		DockerInputPresent        bool
		DockerInputID             int
		MetricResolutionSeconds   float64
	}{
		HostRootPath:              a.hostRootPath,
		LastHealCheck:             a.lastHealCheck,
		LastContainerEventTime:    a.lastContainerEventTime,
		TriggerDiscAt:             a.triggerDiscAt,
		TriggerDiscImmediate:      a.triggerDiscImmediate,
		TriggerFact:               a.triggerFact,
		TriggerSystemUpdateMetric: a.triggerSystemUpdateMetric,
		DockerInputPresent:        a.dockerInputPresent,
		DockerInputID:             a.dockerInputID,
		MetricResolutionSeconds:   a.metricResolution.Seconds(),
	}

	a.triggerLock.Unlock()
	a.l.Unlock()

	enc := json.NewEncoder(file)
	enc.SetIndent("", "  ")

	return enc.Encode(obj)
}

func (a *agent) diagnosticJitter(ctx context.Context, archive types.ArchiveWriter) error {
	file, err := archive.Create("jitter.txt")
	if err != nil {
		return err
	}

	a.l.Lock()
	defer a.l.Unlock()

	fmt.Fprintln(file, "# This file contains time & jitter delay")
	fmt.Fprintln(file, "# A variable jitter may indidate overloaded system")

	var (
		previousTime time.Time
		maxJitter    time.Duration
		avgJitter    time.Duration
	)

	for i, t := range a.watchdogRunAt {
		if i == 0 {
			fmt.Fprintf(file, "run_at=%v jitter=n/a\n", t)
		} else {
			delay := t.Sub(previousTime)
			jitter := delay - time.Minute

			fmt.Fprintf(file, "run_at=%v jitter=%v\n", t, jitter)

			if jitter < 0 {
				jitter = -jitter
			}

			if jitter > maxJitter {
				maxJitter = jitter
			}

			avgJitter += maxJitter
		}

		previousTime = t
	}

	if len(a.watchdogRunAt) > 1 {
		avgJitter /= time.Duration(len(a.watchdogRunAt) - 1)
	}

	fmt.Fprintf(file, "max jitter=%v, avg jitter=%v\n", maxJitter, avgJitter)

	return nil
}

func (a *agent) diagnosticContainers(ctx context.Context, archive types.ArchiveWriter) error {
	file, err := archive.Create("containers.txt")
	if err != nil {
		return err
	}

	containers, err := a.containerRuntime.Containers(ctx, time.Hour, true)
	if err != nil {
		fmt.Fprintf(file, "can't list containers: %v", err)
	} else {
		sort.Slice(containers, func(i, j int) bool {
			return containers[i].ContainerName() < containers[j].ContainerName()
		})

		a.l.Lock()
		lastEvent := a.lastContainerEventTime
		a.l.Unlock()

		fmt.Fprintf(file, "# Containers (count=%d, last update=%s, last event=%s)\n", len(containers), a.containerRuntime.LastUpdate().Format(time.RFC3339), lastEvent.Format(time.RFC3339))

		for _, c := range containers {
			addr, _ := c.ListenAddresses()
			health, healthMsg := c.Health()
			fmt.Fprintf(
				file,
				"Name=%s, ID=%s, ignored=%v, IP=%s, listenAddr=%v,\n\tState=%v, CreatedAt=%v, StartedAt=%v, FinishedAt=%v, StoppedAndReplaced=%v\n\tHealth=%v (%s) K8S=%v/%v\n",
				c.ContainerName(),
				c.ID(),
				facts.ContainerIgnored(c),
				c.PrimaryAddress(),
				addr,
				c.State(),
				c.CreatedAt(),
				c.StartedAt(),
				c.FinishedAt(),
				c.StoppedAndReplaced(),
				health,
				strings.ReplaceAll(healthMsg, "\n", "\\n"),
				c.PodNamespace(),
				c.PodName(),
			)
		}
	}

	return nil
}

func (a *agent) diagnosticSNMP(ctx context.Context, archive types.ArchiveWriter) error {
	file, err := archive.Create("snmp-targets.txt")
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	fmt.Fprintf(file, "# %d SNMP target configured\n", len(a.snmpManager.Targets()))

	for _, t := range a.snmpManager.Targets() {
		fmt.Fprintf(file, "\n%s\n", t.String(ctx))
		facts, err := t.Facts(ctx, 48*time.Hour)

		if err != nil {
			fmt.Fprintf(file, " facts failed: %v\n", err)
		} else {
			for k, v := range facts {
				fmt.Fprintf(file, " * %s = %s\n", k, v)
			}
		}
	}

	if a.bleemeoConnector != nil {
		a.bleemeoConnector.DiagnosticSNMPAssociation(ctx, file)
	}

	return nil
}

func (a *agent) diagnosticConfig(ctx context.Context, archive types.ArchiveWriter) error {
	file, err := archive.Create("config.yaml")
	if err != nil {
		return err
	}

	enc := yaml.NewEncoder(file)

	fmt.Fprintln(file, "# This file contains in-memory configuration used by Glouton. Value from from default, files and environement.")
	enc.SetIndent(4)

	err = enc.Encode(a.oldConfig.Dump())
	if err != nil {
		fmt.Fprintf(file, "# error: %v\n", err)
	}

	err = enc.Close()
	if err != nil {
		fmt.Fprintf(file, "# error: %v\n", err)
	}

	file, err = archive.Create("config-new.yaml")
	if err != nil {
		return err
	}

	enc = yaml.NewEncoder(file)

	fmt.Fprintln(file, "# This file parsed configuration used by Glouton. Currently this config is incomplet.")
	enc.SetIndent(4)

	err = enc.Encode(a.config)
	if err != nil {
		fmt.Fprintf(file, "# error: %v\n", err)
	}

	err = enc.Close()
	if err != nil {
		fmt.Fprintf(file, "# error: %v\n", err)
	}

	return nil
}

func parseIPOutput(content []byte) string {
	lines := strings.Split(string(content), "\n")
	if len(lines) == 0 {
		return ""
	}

	ipRoute := lines[0]
	lines = lines[1:]

	ipAddress := ""
	macAddress := ""

	splitOutput := strings.Split(ipRoute, " ")
	for i, s := range splitOutput {
		if s == "src" && len(splitOutput) > i+1 {
			ipAddress = splitOutput[i+1]
		}
	}

	reNewInterface := regexp.MustCompile(`^\d+: .*$`)
	reEtherAddress := regexp.MustCompile(`^\s+link/ether ([0-9a-fA-F]{2}(:[0-9a-fA-F]{2}){5}) .*`)
	reInetAddress := regexp.MustCompile(`\s+inet (\d+(\.\d+){3})/\d+ .*`)
	currentMacAddress := ""

	for _, line := range lines {
		if reNewInterface.MatchString(line) {
			currentMacAddress = ""
		}

		match := reInetAddress.FindStringSubmatch(line)
		if len(match) > 0 && match[1] == ipAddress {
			macAddress = currentMacAddress

			break
		}

		match = reEtherAddress.FindStringSubmatch(line)
		if len(match) > 0 {
			currentMacAddress = match[1]
		}
	}

	return macAddress
}

// setupContainer will tune container to improve information gathered.
// Mostly it make that access to file pass though hostroot.
func setupContainer(hostRootPath string) {
	if hostRootPath == "" {
		logger.Printf("The agent is running in a container but GLOUTON_DF_HOST_MOUNT_POINT is unset. Some informations will be missing")

		return
	}

	if _, err := os.Stat(hostRootPath); os.IsNotExist(err) {
		logger.Printf("The agent is running in a container but host / partition is not mounted on %#v. Some informations will be missing", hostRootPath)
		logger.Printf("Hint: to fix this issue when using Docker, add \"-v /:%v:ro\" when running the agent", hostRootPath)

		return
	}

	if hostRootPath != "" && hostRootPath != "/" {
		if os.Getenv("HOST_VAR") == "" {
			// gopsutil will use HOST_VAR as prefix to host /var
			// It's used at least for reading the number of connected user from /var/run/utmp
			os.Setenv("HOST_VAR", filepath.Join(hostRootPath, "var"))

			// ... but /var/run is usually a symlink to /run.
			varRun := filepath.Join(hostRootPath, "var/run")
			target, err := os.Readlink(varRun)

			if err == nil && target == "/run" {
				os.Setenv("HOST_VAR", hostRootPath)
			}
		}

		if os.Getenv("HOST_ETC") == "" {
			os.Setenv("HOST_ETC", filepath.Join(hostRootPath, "etc"))
		}

		if os.Getenv("HOST_PROC") == "" {
			os.Setenv("HOST_PROC", filepath.Join(hostRootPath, "proc"))
		}

		if os.Getenv("HOST_SYS") == "" {
			os.Setenv("HOST_SYS", filepath.Join(hostRootPath, "sys"))
		}

		if os.Getenv("HOST_RUN") == "" {
			os.Setenv("HOST_RUN", filepath.Join(hostRootPath, "run"))
		}

		if os.Getenv("HOST_DEV") == "" {
			os.Setenv("HOST_DEV", filepath.Join(hostRootPath, "dev"))
		}

		if os.Getenv("HOST_MOUNT_PREFIX") == "" {
			os.Setenv("HOST_MOUNT_PREFIX", hostRootPath)
		}
	}
}

// prometheusConfigToURLs convert metric.prometheus.targets config to a map of target name to URL
//
// See tests for the expected config.
func prometheusConfigToURLs(cfg interface{}) (result []*scrapper.Target) {
	configList, ok := cfg.([]interface{})
	if !ok {
		return nil
	}

	for _, v := range configList {
		vMap, ok := v.(map[string]interface{})
		if !ok {
			continue
		}

		uText, ok := vMap["url"].(string)
		if !ok {
			continue
		}

		u, err := url.Parse(uText)
		if err != nil {
			logger.Printf("ignoring invalid exporter config: %v", err)

			continue
		}

		name, _ := vMap["name"].(string)

		target := &scrapper.Target{
			ExtraLabels: map[string]string{
				types.LabelMetaScrapeJob: name,
				// HostPort could be empty, but this ExtraLabels is used by Registry which
				// correctly handle empty value value (drop the label).
				types.LabelMetaScrapeInstance: scrapper.HostPort(u),
			},
			URL:       u,
			AllowList: []string{},
			DenyList:  []string{},
		}

		if allow, ok := vMap["allow_metrics"].([]interface{}); ok {
			target.AllowList = make([]string, 0, len(allow))

			for _, x := range allow {
				s, _ := x.(string)
				if s != "" {
					target.AllowList = append(target.AllowList, x.(string))
				}
			}
		}

		denyMetricsConfig(vMap, target)
		result = append(result, target)
	}

	return result
}

func denyMetricsConfig(vMap map[string]interface{}, target *scrapper.Target) {
	if deny, ok := vMap["deny_metrics"].([]interface{}); ok {
		target.DenyList = make([]string, 0, len(deny))

		for _, x := range deny {
			s, _ := x.(string)
			if s != "" {
				target.DenyList = append(target.DenyList, x.(string))
			}
		}
	}
}
