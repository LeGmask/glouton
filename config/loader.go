package config

import (
	"fmt"
	"math"
	"reflect"
	"strings"

	"github.com/knadh/koanf"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/structs"
	"github.com/prometheus/client_golang/prometheus"
)

// configLoader loads the config from Koanf providers.
type configLoader struct {
	items []item
	// Number of provider loaded, used to assign priority to items.
	loadCount int
}

type item struct {
	// The config Key (e.g. "bleemeo.enable").
	Key string
	// The Value for this config key.
	Value interface{}
	// Source of the config key (can be a default value, an environment variable or a file).
	Source source
	// Path to the file the item comes (empty when it doesn't come from a file).
	Path string
	// Priority of the item.
	// When two items have the same key, the one with the highest Priority is kept.
	// When the value is a map or an array, the items may have the same Priority, in
	// this case the arrays are appended to each other, and the maps are merged.
	Priority int
}

type source string

const (
	sourceDefault source = "default"
	sourceEnv     source = "env"
	sourceFile    source = "file"
)

// Load config from a provider and add source information on config items.
func (c *configLoader) Load(path string, provider koanf.Provider, parser koanf.Parser) prometheus.MultiError {
	c.loadCount++

	var warnings prometheus.MultiError

	providerType := providerType(provider)

	k := koanf.New(delimiter)

	err := k.Load(provider, parser)
	warnings.Append(err)

	// Migrate old configuration keys.
	k, moreWarnings := migrate(k)
	warnings = append(warnings, moreWarnings...)

	config, moreWarnings := typedConfig(k)
	warnings = append(warnings, moreWarnings...)

	for key, value := range config {
		priority := c.priority(providerType, key, value, c.loadCount)

		c.items = append(c.items, item{
			Key:      key,
			Value:    value,
			Source:   providerType,
			Path:     path,
			Priority: priority,
		})
	}

	return warnings
}

// typedConfig convert config keys to the right type and returns warnings.
// For details about the type conversions, see the DecodeHook below and mapstructure WeaklyTypedInput.
func typedConfig(
	baseKoanf *koanf.Koanf,
) (map[string]interface{}, prometheus.MultiError) {
	var warnings prometheus.MultiError

	config, err := unmarshalConfig(baseKoanf)
	warnings.Append(err)

	// Convert the structured configuration back to a koanf.
	typedKoanf := koanf.New(delimiter)

	err = typedKoanf.Load(structs.ProviderWithDelim(config, Tag, delimiter), nil)
	warnings.Append(err)

	// Use another koanf to remove keys that were not set in the given config.
	cleanKeys := allKeys(typedKoanf)
	baseKeys := allKeys(baseKoanf)

	for key := range cleanKeys {
		if _, ok := baseKeys[key]; !ok {
			delete(cleanKeys, key)
		}
	}

	return cleanKeys, warnings
}

// allKeys returns all keys from the koanf.
// Map keys are fixed: instead of returning map keys separately
// ("metric.softstatus_period.cpu_used",  "metric.softstatus_period.disk_used"),
// return a single key per map ("metric.softstatus_period").
func allKeys(k *koanf.Koanf) map[string]interface{} {
	all := k.All()

	for key := range all {
		if isMap, mapKey := isMapKey(key); isMap {
			delete(all, key)

			all[mapKey] = k.Get(mapKey)
		}
	}

	return all
}

// priority returns the priority for a provider and a config key value.
// When two items have the same key, the one with the highest priority is kept.
// When the value is a map or an array, the items may have the same priority, in
// this case the arrays are appended to each other, and the maps are merged.
// It panics on unknown providers.
func (c *configLoader) priority(provider source, key string, value interface{}, loadCount int) int {
	const (
		priorityDefault         = -1
		priorityMapAndArrayFile = 1
		priorityEnv             = math.MaxInt
	)

	switch provider {
	case sourceEnv:
		return priorityEnv
	case sourceFile:
		// Slices in files all have the same priority because they are appended.
		if reflect.TypeOf(value).Kind() == reflect.Slice {
			return priorityMapAndArrayFile
		}

		// Map in files all have the same priority because they are merged.
		if isMap, _ := isMapKey(key); isMap {
			return priorityMapAndArrayFile
		}

		// For basic types (string, int, bool, float), the config from the
		// last loaded file has a greater priority than the previous files.
		return loadCount
	case sourceDefault:
		return priorityDefault
	default:
		panic(fmt.Errorf("%w: %T", errUnsupportedProvider, provider))
	}
}

// providerTypes return the provider type from a Koanf provider.
func providerType(provider koanf.Provider) source {
	switch provider.(type) {
	case *env.Env:
		return sourceEnv
	case *file.File:
		return sourceFile
	case *structs.Structs:
		return sourceDefault
	default:
		panic(fmt.Errorf("%w: %T", errUnsupportedProvider, provider))
	}
}

// isMapKey returns true if the config key represents a map value, and the map key.
// For instance: isMapKey("thresholds.cpu_used.low_warning") -> (true, "thresholds").
func isMapKey(key string) (bool, string) {
	for _, mapKey := range mapKeys() {
		// For the map key "thresholds", the key corresponds to this map if the keys
		// are equal or if it begins by the map key and a dot ("thresholds.cpu_used").
		if key == mapKey || strings.HasPrefix(key, fmt.Sprintf("%s.", mapKey)) {
			return true, mapKey
		}
	}

	return false, ""
}

// Build the configuration from the loaded items.
func (c *configLoader) Build() (*koanf.Koanf, prometheus.MultiError) {
	var warnings prometheus.MultiError

	config := make(map[string]interface{})
	priorities := make(map[string]int)

	for _, item := range c.items {
		_, configExists := config[item.Key]
		previousPriority := priorities[item.Key]

		switch {
		// Higher priority items overwrite previous values.
		case !configExists || previousPriority < item.Priority:
			config[item.Key] = item.Value
			priorities[item.Key] = item.Priority
		// Same priority items are merged (slices are appended and maps are merged).
		case previousPriority == item.Priority:
			var err error

			config[item.Key], err = merge(config[item.Key], item.Value)
			warnings.Append(err)
		// Previous item has higher priority, nothing to do.
		case previousPriority > item.Priority:
		}
	}

	k := koanf.New(delimiter)
	err := k.Load(confmap.Provider(config, delimiter), nil)
	warnings.Append(err)

	return k, warnings
}

// Merge maps and append slices.
func merge(dst interface{}, src interface{}) (interface{}, error) {
	switch dstType := dst.(type) {
	case []interface{}:
		return mergeSlices(dstType, src)
	case []string:
		return mergeSlices(dstType, src)
	case map[string]interface{}:
		return mergeMaps(dstType, src)
	case map[string]int:
		return mergeMaps(dstType, src)
	default:
		return nil, fmt.Errorf("%w: unsupported type %T", errCannotMerge, dst)
	}
}

// mergeSlices merges two slices by appending them.
// It returns an error if src doesn't have the same type as dst.
func mergeSlices[T any](dst []T, src interface{}) ([]T, error) {
	srcSlice, ok := src.([]T)
	if !ok {
		return nil, fmt.Errorf("%w: %T and %T are not compatible", errCannotMerge, src, dst)
	}

	return append(dst, srcSlice...), nil
}

// mergeMaps merges two maps.
// It retursn an error if src doesn't have the same type as dst.
func mergeMaps[T any](dst map[string]T, src interface{}) (map[string]T, error) {
	srcMap, ok := src.(map[string]T)
	if !ok {
		return nil, fmt.Errorf("%w: %T and %T are not compatible", errCannotMerge, src, dst)
	}

	for key, value := range srcMap {
		dst[key] = value
	}

	return dst, nil
}
