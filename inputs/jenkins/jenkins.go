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

package jenkins

import (
	"glouton/config"
	"glouton/inputs"
	"glouton/inputs/internal"
	"glouton/types"
	"time"

	"github.com/influxdata/telegraf"
	tConfig "github.com/influxdata/telegraf/config"
	telegraf_inputs "github.com/influxdata/telegraf/plugins/inputs"
	"github.com/influxdata/telegraf/plugins/inputs/jenkins"
)

// New returns a Jenkins input.
func New(config config.Service) (telegraf.Input, *inputs.GathererOptions, error) {
	input, ok := telegraf_inputs.Inputs["jenkins"]
	if !ok {
		return nil, nil, inputs.ErrDisabledInput
	}

	jenkinsInput, ok := input().(*jenkins.Jenkins)
	if !ok {
		return nil, nil, inputs.ErrUnexpectedType
	}

	jenkinsInput.URL = config.StatsURL
	jenkinsInput.Username = config.Username
	jenkinsInput.Password = config.Password
	jenkinsInput.JobInclude = config.Include
	jenkinsInput.JobExclude = config.Exclude

	if config.SSL {
		jenkinsInput.TLSCA = config.CAFile
		jenkinsInput.TLSCert = config.CertFile
		jenkinsInput.TLSKey = config.KeyFile
		jenkinsInput.InsecureSkipVerify = config.SSLInsecure
	}

	// The input writes points in the past (at the date the job started).
	// Limit jobs to process to 5 minutes in the past.
	jenkinsInput.MaxBuildAge = tConfig.Duration(5 * time.Minute)

	// Don't gather node metrics. Metrics for available disk,
	// memory and SWAP are already gathered by Glouton.
	jenkinsInput.NodeExclude = []string{"*"}

	internalInput := &internal.Input{
		Input:       jenkinsInput,
		Accumulator: internal.Accumulator{},
		Name:        "jenkins",
	}

	options := &inputs.GathererOptions{
		MinInterval: 60 * time.Second,
		Rules: []types.SimpleRule{
			{
				TargetName:  "jenkins_job_duration_seconds",
				PromQLQuery: "jenkins_job_duration/1000",
			},
		},
	}

	return internalInput, options, nil
}
