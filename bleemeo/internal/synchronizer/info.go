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

package synchronizer

import (
	"glouton/bleemeo/types"
	"glouton/logger"
	"glouton/version"
)

func (s *Synchronizer) syncInfo(fullSync bool) error {
	// retrieve the min supported glouton version the API supports
	var globalInfo types.GlobalInfo

	_, err := s.client.Do("GET", "v1/info/", nil, nil, &globalInfo)
	// maybe the API does not support this version reporting ? We do not consider this an error for the moment
	if err == nil {
		if !version.Compare(version.Version, globalInfo.Agents.MinVersions.Glouton) {
			logger.V(0).Printf("Your agent is unsupported, consider upgrading it (got version %s, expected version >= %s)", version.Version, globalInfo.Agents.MinVersions.Glouton)
			return &types.ErrShutdownRequested{Reason: types.DisableAgentTooOld}
		}

		if globalInfo.ReadOnly {
			if !s.option.MqttIsReadOnly() {
				logger.V(0).Println("MQTT: read only mode enabled")
				s.option.MqttSetReadOnly(true)
			}
		} else {
			if s.option.MqttIsReadOnly() {
				logger.V(0).Println("MQTT: read only mode is now disabled, will resume sending metrics")
				s.option.MqttSetReadOnly(false)
			}
		}
	}

	if err != nil {
		logger.V(2).Printf("Couldn't retrieve global informations, got %v", err)
	}

	return nil
}
