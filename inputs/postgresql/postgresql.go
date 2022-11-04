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

package postgresql

import (
	"glouton/inputs"
	"glouton/inputs/internal"

	"github.com/influxdata/telegraf"
	telegraf_inputs "github.com/influxdata/telegraf/plugins/inputs"
	"github.com/influxdata/telegraf/plugins/inputs/postgresql"
)

// New initialise postgresql.Input.
func New(address string, detailedDatabases []string) (telegraf.Input, error) {
	input, ok := telegraf_inputs.Inputs["postgresql"]
	if !ok {
		return nil, inputs.ErrDisabledInput
	}

	postgresqlInput, ok := input().(*postgresql.Postgresql)
	if !ok {
		return nil, inputs.ErrUnexpectedType
	}

	postgresqlInput.Address = address
	postgresqlInput.Databases = detailedDatabases

	globalMetricsInput := globalMetrics{
		input: postgresqlInput,
	}

	internalInput := &internal.Input{
		Input: globalMetricsInput,
		Accumulator: internal.Accumulator{
			RenameGlobal: renameGlobal(detailedDatabases),
			DerivatedMetrics: []string{
				"xact_commit", "xact_rollback", "blks_read", "blks_hit", "tup_returned", "tup_fetched",
				"tup_inserted", "tup_updated", "tup_deleted", "temp_files", "temp_bytes", "blk_read_time",
				"blk_write_time",
			},
			TransformMetrics: transformMetrics,
		},
		Name: "postgresql",
	}

	return internalInput, nil
}

func renameGlobal(detailedDatabases []string) func(internal.GatherContext) (internal.GatherContext, bool) {
	// Gather sum metrics only if no detailed database is set in the config.
	if len(detailedDatabases) == 0 {
		detailedDatabases = []string{"global"}
	}

	return func(gatherContext internal.GatherContext) (internal.GatherContext, bool) {
		for _, db := range detailedDatabases {
			if db == gatherContext.Tags["db"] {
				gatherContext.Annotations.BleemeoItem = gatherContext.Tags["db"]

				return gatherContext, false
			}
		}

		return gatherContext, true
	}
}

func transformMetrics(
	currentContext internal.GatherContext,
	fields map[string]float64,
	originalFields map[string]interface{},
) map[string]float64 {
	newFields := make(map[string]float64)

	for metricName, value := range fields {
		switch metricName {
		case "xact_commit":
			newFields["commit"] = value
		case "xact_rollback":
			newFields["rollback"] = value
		case "blks_read", "blks_hit", "tup_returned", "tup_fetched", "tup_inserted", "tup_updated":
			newFields[metricName] = value
		case "tup_deleted", "temp_files", "temp_bytes", "blk_read_time", "blk_write_time":
			newFields[metricName] = value
		}
	}

	return newFields
}
