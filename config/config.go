package config

import (
	"errors"
	"fmt"
	"glouton/logger"
	"os"
	"path/filepath"
	"strings"

	"github.com/imdario/mergo"
	"github.com/knadh/koanf"
	yamlParser "github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/structs"
	"github.com/mitchellh/mapstructure"
	"gopkg.in/yaml.v3"
)

const (
	envPrefix           = "GLOUTON_"
	deprecatedEnvPrefix = "BLEEMEO_AGENT_"
	delimiter           = "."
)

var (
	errDeprecatedEnv      = errors.New("environment variable is deprecated")
	errSettingsDeprecated = errors.New("setting is deprecated")
	errWrongMapFormat     = errors.New("could not parse map from string")
	ErrInvalidValue       = errors.New("invalid config value")
)

type Warnings []error

// Load loads the configuration from files and directories to a struct.
func Load(withDefault bool, paths ...string) (Config, Warnings, error) {
	// If no config was given with flags or env variables, fallback on the default files.
	if len(paths) == 0 || len(paths) == 1 && paths[0] == "" {
		paths = DefaultPaths()
	}

	return loadToStruct(withDefault, paths...)
}

func loadToStruct(withDefault bool, paths ...string) (Config, Warnings, error) {
	// Override config files if the files were given from the env.
	if envFiles := os.Getenv("GLOUTON_CONFIG_FILES"); envFiles != "" {
		paths = strings.Split(envFiles, ",")
	}

	k, warnings, err := load(withDefault, paths...)

	var config Config

	// We need to use the "yaml" tag instead of the default "koanf" tag because
	// the config embeds the blackbox module config which uses YAML.
	unmarshalConf := koanf.UnmarshalConf{
		DecoderConfig: &mapstructure.DecoderConfig{
			DecodeHook: mapstructure.ComposeDecodeHookFunc(
				mapstructure.StringToTimeDurationHookFunc(),
				mapstructure.StringToSliceHookFunc(","),
				mapstructure.TextUnmarshallerHookFunc(),
				blackboxModuleHookFunc(),
				stringToMapHookFunc(),
				stringToBoolHookFunc(),
			),
			Metadata:         nil,
			ErrorUnused:      true,
			Result:           &config,
			WeaklyTypedInput: true,
		},
		Tag: "yaml",
	}

	if warning := k.UnmarshalWithConf("", &config, unmarshalConf); warning != nil {
		warnings = append(warnings, warning)
	}

	return config, unwrapErrors(warnings), err
}

// load the configuration from files and directories.
func load(withDefault bool, paths ...string) (*koanf.Koanf, Warnings, error) {
	var finalErr error

	fileEnvKoanf, warnings, finalErr := loadPaths(paths)

	fileEnvKoanf, moreWarnings := migrate(fileEnvKoanf)
	if moreWarnings != nil {
		warnings = append(warnings, moreWarnings...)
	}

	// Load config from environment variables.
	// The warnings are filled only after k.Load is called.
	envToKey, envWarnings := envToKeyFunc()

	// Environment variable overwrite basic types (string, int), arrays, and maps.
	envMergeFunc := mergeFunc(mergo.WithOverride)

	if err := fileEnvKoanf.Load(env.Provider(deprecatedEnvPrefix, delimiter, envToKey), nil, envMergeFunc); err != nil {
		warnings = append(warnings, err)
	}

	if err := fileEnvKoanf.Load(env.Provider(envPrefix, delimiter, envToKey), nil, envMergeFunc); err != nil {
		warnings = append(warnings, err)
	}

	if len(*envWarnings) > 0 {
		warnings = append(warnings, *envWarnings...)
	}

	// Load default values.
	k := koanf.New(delimiter)

	if withDefault {
		err := k.Load(structsProvider(DefaultConfig(), "yaml"), nil)
		if err != nil {
			finalErr = err
		}
	}

	// Merge defaults and config from files and environment.
	// The config overwrites the defaults for basic types (string, int) and arrays, and merges maps.
	if err := k.Load(confmap.Provider(fileEnvKoanf.All(), delimiter), nil, mergeFunc(mergo.WithOverride)); err != nil {
		warnings = append(warnings, err)
	}

	return k, warnings, finalErr
}

// envToKeyFunc returns a function that converts an environment variable to a configuration key
// and a pointer to Warnings, the warnings are filled only after koanf.Load has been called.
// Panics if two config keys correspond to the same environment variable.
func envToKeyFunc() (func(string) string, *Warnings) {
	// Get all config keys from an empty config.
	k := koanf.New(delimiter)
	_ = k.Load(structs.Provider(Config{}, "yaml"), nil)
	allKeys := k.All()

	// Build a map of the environment variables with their corresponding config keys.
	envToKey := make(map[string]string, len(allKeys))

	for key := range allKeys {
		envKey := toEnvKey(key)

		if oldKey, exists := envToKey[envKey]; exists {
			panic(fmt.Sprintf("Conflict between config keys, %s and %s both corresponds to the variable %s", oldKey, key, envKey))
		}

		envToKey[envKey] = key
	}

	// Build a map of the deprecated environment variables with their corresponding new variable.
	movedEnvKeys := map[string]string{
		"BLEEMEO_AGENT_ACCOUNT":          "GLOUTON_BLEEMEO_ACCOUNT_ID",
		"BLEEMEO_AGENT_REGISTRATION_KEY": "GLOUTON_BLEEMEO_REGISTRATION_KEY",
		"BLEEMEO_AGENT_API_BASE":         "GLOUTON_BLEEMEO_API_BASE",
		"BLEEMEO_AGENT_MQTT_HOST":        "GLOUTON_BLEEMEO_MQTT_HOST",
		"BLEEMEO_AGENT_MQTT_PORT":        "GLOUTON_BLEEMEO_MQTT_PORT",
		"BLEEMEO_AGENT_MQTT_SSL":         "GLOUTON_BLEEMEO_MQTT_SSL",
	}

	for k, v := range movedKeys() {
		movedEnvKeys[toEnvKey(k)] = toEnvKey(v)
		movedEnvKeys[toDeprecatedEnvKey(k)] = toEnvKey(v)
	}

	warnings := make(Warnings, 0)
	envFunc := func(s string) string {
		// Migrate deprecated keys.
		if newKey, ok := movedEnvKeys[s]; ok {
			warnings = append(warnings, fmt.Errorf("%w: %s, use %s instead", errDeprecatedEnv, s, newKey))
			s = newKey
		}

		if strings.HasPrefix(s, deprecatedEnvPrefix) {
			newKey := strings.Replace(s, deprecatedEnvPrefix, envPrefix, 1)
			warnings = append(warnings, fmt.Errorf("%w: %s, use %s instead", errDeprecatedEnv, s, newKey))
			s = newKey
		}

		return envToKey[s]
	}

	return envFunc, &warnings
}

// mergeFunc return a merge function to use with koanf.
func mergeFunc(opts ...func(*mergo.Config)) koanf.Option {
	merge := func(src, dest map[string]interface{}) error {
		err := mergo.Merge(&dest, src, opts...)
		if err != nil {
			logger.Printf("Error merging config: %s", err)
		}

		return err
	}

	return koanf.WithMergeFunc(merge)
}

// toEnvKey returns the environment variable corresponding to a configuration key.
// For instance: toEnvKey("web.enable") -> GLOUTON_WEB_ENABLE.
func toEnvKey(key string) string {
	envKey := strings.ToUpper(key)
	envKey = envPrefix + strings.ReplaceAll(envKey, ".", "_")

	return envKey
}

// toDeprecatedEnvKey returns the environment variable corresponding to a configuration key
// with the deprecated prefix. For instance: toEnvKey("web.enable") -> BLEEMEO_AGENT_WEB_ENABLE.
func toDeprecatedEnvKey(key string) string {
	envKey := strings.ToUpper(key)
	envKey = deprecatedEnvPrefix + strings.ReplaceAll(envKey, ".", "_")

	return envKey
}

func loadPaths(paths []string) (*koanf.Koanf, Warnings, error) {
	var (
		finalError error
		warnings   Warnings
	)

	k := koanf.New(delimiter)

	for _, path := range paths {
		stat, err := os.Stat(path)
		if err != nil && os.IsNotExist(err) {
			logger.V(2).Printf("config file: %s ignored since it does not exists", path)

			continue
		}

		if err != nil {
			logger.V(2).Printf("config file: %s ignored due to %v", path, err)

			finalError = err

			continue
		}

		if stat.IsDir() {
			moreWarnings, err := loadDirectory(k, path)
			if err != nil {
				finalError = err
			}

			if moreWarnings != nil {
				warnings = append(warnings, moreWarnings...)
			}

			if err != nil {
				logger.V(2).Printf("config file: directory %s have ignored some files due to %v", path, err)
			}
		} else {
			warning := loadFile(k, path)
			if warning != nil {
				warnings = append(warnings, warning)
			}
		}

		if err == nil {
			logger.V(2).Printf("config file: %s loaded", path)
		}
	}

	return k, warnings, finalError
}

func loadDirectory(k *koanf.Koanf, dirPath string) (Warnings, error) {
	files, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, err
	}

	var warnings Warnings

	for _, f := range files {
		if !strings.HasSuffix(f.Name(), ".conf") {
			continue
		}

		path := filepath.Join(dirPath, f.Name())

		warning := loadFile(k, path)
		if warning != nil {
			warnings = append(warnings, warning)
		}
	}

	return warnings, nil
}

func loadFile(k *koanf.Koanf, path string) error {
	// Merge this file with the previous config.
	// Overwrite values, merge maps and append slices.
	err := k.Load(file.Provider(path), yamlParser.Parser(), mergeFunc(mergo.WithOverride, mergo.WithAppendSlice))
	if err != nil {
		return fmt.Errorf("failed to load '%s': %w", path, err)
	}

	return nil
}

// unwrapErrors unwrap all errors in the list than contain multiple errors.
func unwrapErrors(errs []error) []error {
	if len(errs) == 0 {
		return nil
	}

	unwrapped := make([]error, 0, len(errs))

	for _, err := range errs {
		var (
			mapErr  *mapstructure.Error
			yamlErr *yaml.TypeError
		)

		switch {
		case errors.As(err, &mapErr):
			unwrapped = append(unwrapped, mapErr.WrappedErrors()...)
		case errors.As(err, &yamlErr):
			for _, wrappedErr := range yamlErr.Errors {
				unwrapped = append(unwrapped, errors.New(wrappedErr)) //nolint:goerr113
			}
		default:
			unwrapped = append(unwrapped, err)
		}
	}

	return unwrapped
}

// movedKeys return all keys that were moved. The map is old key => new key.
func movedKeys() map[string]string {
	keys := map[string]string{
		"agent.windows_exporter.enabled":  "agent.windows_exporter.enable",
		"agent.http_debug.enabled":        "agent.http_debug.enable",
		"kubernetes.enabled":              "kubernetes.enable",
		"blackbox.enabled":                "blackbox.enable",
		"agent.process_exporter.enabled":  "agent.process_exporter.enable",
		"web.enabled":                     "web.enable",
		"bleemeo.enabled":                 "bleemeo.enable",
		"jmx.enabled":                     "jmx.enable",
		"nrpe.enabled":                    "nrpe.enable",
		"zabbix.enabled":                  "zabbix.enable",
		"influxdb.enabled":                "influxdb.enable",
		"telegraf.statsd.enabled":         "telegraf.statsd.enable",
		"agent.telemetry.enabled":         "agent.telemetry.enable",
		"agent.node_exporter.enabled":     "agent.node_exporter.enable",
		"telegraf.docker_metrics_enabled": "telegraf.docker_metrics_enable",
	}

	return keys
}

// migrate upgrade the configuration when Glouton changes its settings.
func migrate(k *koanf.Koanf) (*koanf.Koanf, Warnings) {
	config := k.All()

	var warnings Warnings

	warnings = append(warnings, migrateMovedKeys(k, config)...)
	warnings = append(warnings, migrateLogging(k, config)...)
	warnings = append(warnings, migrateMetricsPrometheus(k, config)...)
	warnings = append(warnings, migrateScrapperMetrics(k, config)...)

	// We can't reuse the previous Koanf because it doesn't allow removing keys.
	newConfig := koanf.New(delimiter)

	err := newConfig.Load(confmap.Provider(config, delimiter), nil)
	if err != nil {
		warnings = append(warnings, err)
	}

	return newConfig, warnings
}

// migrateMovedKeys migrate the config settings that were simply moved.
func migrateMovedKeys(k *koanf.Koanf, config map[string]interface{}) Warnings {
	var warnings Warnings //nolint:prealloc // False positive.

	keys := movedKeys()

	for oldKey, newKey := range keys {
		val := k.Get(oldKey)
		if val == nil {
			continue
		}

		config[newKey] = val
		delete(config, oldKey)

		warnings = append(warnings, fmt.Errorf("%w: %s, use %s instead", errSettingsDeprecated, oldKey, newKey))
	}

	return warnings
}

// migrateLogging migrates the logging settings.
func migrateLogging(k *koanf.Koanf, config map[string]interface{}) (warnings []error) {
	for _, name := range []string{"tail_size", "head_size"} {
		oldKey := "logging.buffer." + name
		newKey := "logging.buffer." + name + "_bytes"

		value := k.Int(oldKey)
		if value == 0 {
			continue
		}

		config[newKey] = value * 100
		delete(config, oldKey)

		warnings = append(warnings, fmt.Errorf("%w: %s, use %s instead", errSettingsDeprecated, oldKey, newKey))
	}

	return warnings
}

// migrateMetricsPrometheus migrates Prometheus settings.
func migrateMetricsPrometheus(k *koanf.Koanf, config map[string]interface{}) Warnings {
	// metrics.prometheus was renamed metrics.prometheus.targets
	// We guess that old path was used when metrics.prometheus.*.url exist and is a string
	v := k.Get("metric.prometheus")
	if v == nil {
		return nil
	}

	var ( //nolint:prealloc // False positive.
		warnings        Warnings
		migratedTargets []interface{}
	)

	vMap, ok := v.(map[string]interface{})
	if !ok {
		return nil
	}

	for key, dict := range vMap {
		tmp, ok := dict.(map[string]interface{})
		if !ok {
			continue
		}

		u, ok := tmp["url"].(string)
		if !ok {
			continue
		}

		warnings = append(warnings, fmt.Errorf("%w: metrics.prometheus. See https://docs.bleemeo.com/metrics-sources/prometheus", errSettingsDeprecated))

		migratedTargets = append(migratedTargets, map[string]interface{}{
			"url":  u,
			"name": key,
		})

		delete(config, fmt.Sprintf("metric.prometheus.%s", key))
		delete(config, fmt.Sprintf("metric.prometheus.%s.url", key))
		delete(config, fmt.Sprintf("metric.prometheus.%s.name", key))
	}

	if len(migratedTargets) > 0 {
		existing := k.Get("metric.prometheus.targets")
		targets, _ := existing.([]interface{})
		targets = append(targets, migratedTargets...)

		config["metric.prometheus.targets"] = targets
	}

	if k.Bool("metric.prometheus.targets.include_default_metrics") {
		warnings = append(warnings, fmt.Errorf("%w: metrics.prometheus.targets.include_default_metrics. This option does not exists anymore and has no effect", errSettingsDeprecated))
	}

	return warnings
}

func migrateScrapperMetrics(k *koanf.Koanf, config map[string]interface{}) Warnings {
	var warnings Warnings

	warnings = append(warnings, migrateScrapper(k, config, "metric.prometheus.allow_metrics", "metric.allow_metrics")...)
	warnings = append(warnings, migrateScrapper(k, config, "metric.prometheus.deny_metrics", "metric.deny_metrics")...)
	warnings = append(warnings, migrateScrapper(k, config, "metric.prometheus.allow", "metric.allow_metrics")...)
	warnings = append(warnings, migrateScrapper(k, config, "metric.prometheus.deny", "metric.deny_metrics")...)

	return warnings
}

func migrateScrapper(k *koanf.Koanf, config map[string]interface{}, deprecatedPath string, correctPath string) Warnings {
	migratedTargets := []string{}
	v := k.Get(deprecatedPath)

	if v == nil {
		return nil
	}

	vTab, ok := v.([]interface{})
	if !ok {
		return nil
	}

	var warnings Warnings

	if len(vTab) > 0 {
		warnings = append(warnings, fmt.Errorf("%w: %s. Please use %s", errSettingsDeprecated, deprecatedPath, correctPath))

		for _, val := range vTab {
			s, _ := val.(string)
			if s != "" {
				migratedTargets = append(migratedTargets, s)
			}
		}
	}

	if len(migratedTargets) > 0 {
		existing := k.Get(correctPath)
		targets, _ := existing.([]interface{})

		for _, val := range migratedTargets {
			targets = append(targets, val)
		}

		config[correctPath] = targets
		delete(config, deprecatedPath)
	}

	return warnings
}

// Dump return a copy of the whole configuration, with secrets retracted.
// secret is any key containing "key", "secret", "password" or "passwd".
func Dump(config Config) map[string]interface{} {
	k := koanf.New(delimiter)
	_ = k.Load(structs.Provider(config, "yaml"), nil)

	return dump(k.All())
}

func dump(root map[string]interface{}) map[string]interface{} {
	secretKey := []string{"key", "secret", "password", "passwd"}

	for k, v := range root {
		isSecret := false

		for _, name := range secretKey {
			if strings.Contains(k, name) {
				isSecret = true

				break
			}
		}

		if isSecret {
			root[k] = "*****"

			continue
		}

		switch v := v.(type) {
		case map[string]interface{}:
			root[k] = dump(v)
		case []interface{}:
			root[k] = dumpList(v)
		default:
			root[k] = v
		}
	}

	return root
}

func dumpList(root []interface{}) []interface{} {
	for i, v := range root {
		switch v := v.(type) {
		case map[string]interface{}:
			root[i] = dump(v)
		case []interface{}:
			root[i] = dumpList(v)
		default:
			root[i] = v
		}
	}

	return root
}
