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

package synchronizer

import (
	"context"
	"encoding/json"
	"fmt"
	"glouton/bleemeo/client"
	bleemeoTypes "glouton/bleemeo/types"
	"glouton/config"
	"glouton/logger"
	"reflect"
)

// comparableConfigItem is a modified GloutonConfigItem without the
// interface{} value to make it comparable.
type comparableConfigItem struct {
	Key      string
	Priority int
	Source   bleemeoTypes.ConfigItemSource
	Path     string
	Type     bleemeoTypes.ConfigItemType
}

type configItemValue struct {
	ID    string
	Value interface{}
}

func (s *Synchronizer) syncConfig(
	ctx context.Context,
	fullSync bool,
	onlyEssential bool,
) (updateThresholds bool, err error) {
	// The config is not essential and synchronisation is done only during full sync.
	if onlyEssential || !fullSync {
		return false, nil
	}

	remoteConfigItems, err := s.fetchAllConfigItems(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to fetch config items: %w", err)
	}

	localConfigItems := s.localConfigItems()

	// Registers local config items not present on the API.
	err = s.registerLocalConfigItems(ctx, localConfigItems, remoteConfigItems)
	if err != nil {
		return false, err
	}

	// Remove remote config items not present locally.
	err = s.removeRemoteConfigItems(ctx, localConfigItems, remoteConfigItems)
	if err != nil {
		return false, err
	}

	return false, nil
}

// fetchAllConfigItems returns the remote config items in a map of config value by comparableConfigItem.
func (s *Synchronizer) fetchAllConfigItems(ctx context.Context) (map[comparableConfigItem]configItemValue, error) {
	params := map[string]string{
		"fields": "id,agent,key,value,priority,source,path,type",
		"agent":  s.agentID,
	}

	result, err := s.client.Iter(ctx, "gloutonconfigitem", params)
	if err != nil {
		return nil, fmt.Errorf("client iter: %w", err)
	}

	items := make(map[comparableConfigItem]configItemValue, len(result))

	for _, jsonMessage := range result {
		var item bleemeoTypes.GloutonConfigItem

		if err := json.Unmarshal(jsonMessage, &item); err != nil {
			logger.V(2).Printf("Failed to unmarshal config item: %v", err)

			continue
		}

		key := comparableConfigItem{
			Key:      item.Key,
			Priority: item.Priority,
			Source:   item.Source,
			Path:     item.Path,
			Type:     item.Type,
		}

		items[key] = configItemValue{
			ID:    item.ID,
			Value: item.Value,
		}
	}

	return items, nil
}

// localConfigItems returns the local config items in a map of config value by comparableConfigItem.
func (s *Synchronizer) localConfigItems() map[comparableConfigItem]interface{} {
	items := make(map[comparableConfigItem]interface{}, len(s.option.ConfigItems))

	for _, item := range s.option.ConfigItems {
		key := comparableConfigItem{
			Key:      shortenCharField(item.Key),
			Priority: item.Priority,
			Source:   bleemeoItemSourceFromConfigSource(item.Source),
			Path:     shortenCharField(item.Path),
			Type:     bleemeoItemTypeFromConfigValue(item.Value),
		}

		items[key] = item.Value
	}

	return items
}

// Shorten a field to make it registerable on the API.
func shortenCharField(field string) string {
	const maxChar = 100

	if len(field) <= maxChar {
		return field
	}

	return field[:maxChar]
}

func bleemeoItemSourceFromConfigSource(source config.Source) bleemeoTypes.ConfigItemSource {
	switch source {
	case config.SourceFile:
		return bleemeoTypes.SourceFile
	case config.SourceEnv:
		return bleemeoTypes.SourceEnv
	case config.SourceDefault:
		return bleemeoTypes.SourceDefault
	default:
		return bleemeoTypes.SourceUnknown
	}
}

func bleemeoItemTypeFromConfigValue(value interface{}) bleemeoTypes.ConfigItemType {
	switch value.(type) {
	case int:
		return bleemeoTypes.TypeInt
	case float64:
		return bleemeoTypes.TypeFloat
	case bool:
		return bleemeoTypes.TypeBool
	case string:
		return bleemeoTypes.TypeString
	}

	// For slices and maps of any types, we need reflection.
	if value == nil {
		return bleemeoTypes.TypeUnknown
	}

	switch reflect.TypeOf(value).Kind() { //nolint:exhaustive
	case reflect.Slice:
		return bleemeoTypes.TypeList
	case reflect.Map:
		return bleemeoTypes.TypeMap
	}

	return bleemeoTypes.TypeUnknown
}

// registerLocalConfigItems registers local config items not present on the API.
func (s *Synchronizer) registerLocalConfigItems(
	ctx context.Context,
	localConfigItems map[comparableConfigItem]interface{},
	remoteConfigItems map[comparableConfigItem]configItemValue,
) error {
	// Find and register local items that are not present on the API.
	for localItem, localValue := range localConfigItems {
		remoteItem, ok := remoteConfigItems[localItem]

		// Skip items that already exist on the API.
		if ok && reflect.DeepEqual(localValue, remoteItem.Value) {
			continue
		}

		// Register the new item.
		item := bleemeoTypes.GloutonConfigItem{
			Agent:    s.agentID,
			Key:      localItem.Key,
			Value:    localValue,
			Priority: localItem.Priority,
			Source:   localItem.Source,
			Path:     localItem.Path,
			Type:     localItem.Type,
		}

		_, err := s.client.Do(ctx, "POST", "v1/gloutonconfigitem/", nil, item, nil)
		if err != nil {
			return err
		}
	}

	return nil
}

// removeRemoteConfigItems removes remote config items not present locally.
func (s *Synchronizer) removeRemoteConfigItems(
	ctx context.Context,
	localConfigItems map[comparableConfigItem]interface{},
	remoteConfigItems map[comparableConfigItem]configItemValue,
) error {
	// Find and remove remote items that are not present locally.
	for remoteKey, remoteItem := range remoteConfigItems {
		localValue, ok := localConfigItems[remoteKey]

		// Skip items that already exist locally.
		if ok && reflect.DeepEqual(localValue, remoteItem.Value) {
			continue
		}

		_, err := s.client.Do(ctx, "DELETE", fmt.Sprintf("v1/gloutonconfigitem/%s/", remoteItem.ID), nil, nil, nil)
		if err != nil {
			// Ignore the error if the item has already been deleted.
			if client.IsNotFound(err) {
				continue
			}

			return err
		}
	}

	return nil
}
