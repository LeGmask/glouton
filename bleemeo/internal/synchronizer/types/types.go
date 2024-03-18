// Copyright 2015-2023 Bleemeo
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

package types

import (
	"context"
	"encoding/json"
	"glouton/bleemeo/internal/cache"
	bleemeoTypes "glouton/bleemeo/types"
	"glouton/prometheus/model"
	"io"
	"time"
)

type EntityName string

const (
	EntityInfo          EntityName = "info"
	EntityAgent         EntityName = "agent"
	EntityAccountConfig EntityName = "accountconfig"
	EntityMonitor       EntityName = "monitor"
	EntitySNMP          EntityName = "snmp"
	EntityVSphere       EntityName = "vsphere"
	EntityFact          EntityName = "facts"
	EntityService       EntityName = "service"
	EntityContainer     EntityName = "container"
	EntityMetric        EntityName = "metric"
	EntityConfig        EntityName = "config"
	EntityDiagnostics   EntityName = "diagnostics"
)

type SyncType int

const (
	SyncTypeNone              SyncType = 0
	SyncTypeNormal            SyncType = 1
	SyncTypeForceCacheRefresh SyncType = 2
)

type EntitySynchronizer interface {
	Name() EntityName
	// EnabledInMaintenance return True if this entity should be synchronized even in maintenance mode
	EnabledInMaintenance() bool
	// EnabledInSuspendedMode return True if this entity should be synchronized even in suspended mode
	EnabledInSuspendedMode() bool

	PrepareExecution(ctx context.Context, execution SynchronizationExecution) (EntitySynchronizerExecution, error)
}

type EntitySynchronizerExecution interface {
	// NeedSynchronization returns True if the entity need to be synchronized. This could depends
	// on various state of the Glouton.
	// Note that the synchronized could call the entity synchronizer even if the method return false,
	// mostly in case a RequestSynchronization() was called.
	// It could also NOT call it even if the method return true, mostly in case an error occurred in another
	// synchronizer.
	NeedSynchronization(ctx context.Context) bool

	// RefreshCache update the cache with information from Bleemeo API. This could no nothing
	// if the cache is still valid.
	RefreshCache(ctx context.Context, syncType SyncType) error

	// SyncRemoteAndLocal update the Bleemeo API and/or the local entity list: create, update or delete entries.
	// RefreshCache normally update the Bleemeo API *cache*, not the Glouton local entity list.
	// For example for services entity, the cache the a view of Bleemeo API and the local list is the output of
	// discovery.
	SyncRemoteAndLocal(ctx context.Context, syncType SyncType) error

	// FinishExecution is called at the end of execution of all entities synchronization (NeedSynchronization,
	// RefreshCache and SyncRemoteAndLocal).
	// It's called unconditionally if PrepareExecution() returned non-nil value. Even if NeedSynchronization() returned false,
	// or a function had error.
	FinishExecution(ctx context.Context)
}

type SynchronizationExecution interface {
	BleemeoAPIClient() Client
	IsOnlyEssential() bool
	// RequestSynchronization ask for a execution of synchronization of specified entity.
	// If this is called during calls to NeedSynchronization, it's tried to be run during
	// current execution of synchronization (no guarantee, e.g. on error).
	// If called later, once SyncRemote start being called, it will be run during *next* execution.
	RequestSynchronization(entityName EntityName, requestFull bool)
	// RequestSynchronizationForAll calls RequestSynchronization for all known entities.
	RequestSynchronizationForAll(requestFull bool)
	// IsSynchronizationExplicitlyRequested return whether a synchronization was requested for the
	// specific entity during current execution.
	// Note: even if this method return false, a synchronization might occur when ForceCacheRefreshForAll() is true
	IsSynchronizationExplicitlyRequested(entityName EntityName) bool
	RequestUpdateThresholds()
	RequestNotifyLabelsUpdate()
	Option() Option
	// Time the last synchronization started and completed without error on all entities.
	LastSync() time.Time
	// Time this synchronization started
	StartedAt() time.Time
	GlobalState() SynchronizedGlobalState
}

type Client interface {
	RawClient
}

// RawClient a client doing generic HTTP call to Bleemeo API.
// Ideally synchronizer of entity should move to higher level interface, which will make mocking easier (like ListActiveMetrics, CreateService...)
type RawClient interface {
	Do(ctx context.Context, method string, path string, params map[string]string, data interface{}, result interface{}) (statusCode int, err error)
	DoWithBody(ctx context.Context, path string, contentType string, body io.Reader) (statusCode int, err error)
	Iter(ctx context.Context, resource string, params map[string]string) ([]json.RawMessage, error)
}

type SynchronizedGlobalState interface {
	IsMaintenance() bool
}

// Option are parameters for the synchronizer.
type Option struct {
	bleemeoTypes.GlobalOption
	Cache        *cache.Cache
	PushAppender *model.BufferAppender

	// DisableCallback is a function called when Synchronizer request Bleemeo connector to be disabled
	// reason state why it's disabled and until set for how long it should be disabled.
	DisableCallback func(reason bleemeoTypes.DisableReason, until time.Time)

	// UpdateConfigCallback is a function called when Synchronizer detected a AccountConfiguration change
	UpdateConfigCallback func(ctx context.Context, nameChanged bool)

	// SetInitialized tells the bleemeo connector that the MQTT module can be started
	SetInitialized func()

	// IsMqttConnected returns whether the MQTT connector is operating nominally, and specifically
	// that it can receive mqtt notifications. It is useful for the fallback on http polling
	// described above Synchronizer.lastMaintenanceSync definition.
	// Note: returns false when the mqtt connector is not enabled.
	IsMqttConnected func() bool

	// SetBleemeoInMaintenanceMode makes the bleemeo connector wait a day before checking again for maintenance.
	SetBleemeoInMaintenanceMode func(ctx context.Context, maintenance bool)

	// SetBleemeoInSuspendedMode sets the suspended mode. While Bleemeo is suspended the agent doesn't
	// create or update objects on the API and stops sending points on MQTT. The suspended mode differs
	// from the maintenance mode because we stop buffering points to send on MQTT and just drop them.
	SetBleemeoInSuspendedMode func(suspended bool)
}
