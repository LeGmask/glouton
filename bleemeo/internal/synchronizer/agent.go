// Copyright 2015-2024 Bleemeo
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
	"errors"
	"fmt"

	"github.com/bleemeo/glouton/bleemeo/internal/synchronizer/types"
	bleemeoTypes "github.com/bleemeo/glouton/bleemeo/types"
	"github.com/bleemeo/glouton/logger"
)

var errNoConfig = errors.New("agent don't have any configuration on Bleemeo Cloud platform. Please contact support@bleemeo.com about this issue")

const (
	apiTagsLength = 100
	agentFields   = "account,agent_type,created_at,current_config,display_name,fqdn,id,is_cluster_leader,next_config_at,tags"
)

func (s *Synchronizer) syncAgent(ctx context.Context, syncType types.SyncType, execution types.SynchronizationExecution) (updateThresholds bool, err error) {
	apiClient := execution.BleemeoAPIClient()

	if err := s.syncMainAgent(ctx, apiClient); err != nil {
		return false, err
	}

	if execution.IsOnlyEssential() {
		return false, nil
	}

	if syncType == types.SyncTypeForceCacheRefresh {
		if err := s.agentsUpdateList(ctx, execution, apiClient); err != nil {
			return false, err
		}
	}

	return false, nil
}

func (s *Synchronizer) syncMainAgent(ctx context.Context, apiClient types.RawClient) error {
	var agent bleemeoTypes.Agent

	params := map[string]string{
		"fields": agentFields,
	}
	data := map[string][]bleemeoTypes.Tag{
		"tags": make([]bleemeoTypes.Tag, 0),
	}

	for _, t := range s.option.Config.Tags {
		if len(t) <= apiTagsLength && t != "" {
			data["tags"] = append(data["tags"], bleemeoTypes.Tag{Name: t})
		}
	}

	previousAgent := s.option.Cache.Agent()

	_, err := apiClient.Do(ctx, "PATCH", fmt.Sprintf("v1/agent/%s/", s.agentID), params, data, &agent)
	if err != nil {
		return err
	}

	s.option.Cache.SetAgent(agent)

	if agent.CurrentConfigID == "" {
		return errNoConfig
	}

	s.option.Cache.SetAccountID(agent.AccountID)

	if agent.AccountID != s.option.Config.Bleemeo.AccountID && !s.warnAccountMismatchDone {
		s.warnAccountMismatchDone = true
		logger.Printf(
			"Account ID in configuration file (%s) mismatch the current account ID (%s). The Account ID from configuration file will be ignored.",
			s.option.Config.Bleemeo.AccountID,
			agent.AccountID,
		)
	}

	if agent.IsClusterLeader && !previousAgent.IsClusterLeader {
		logger.V(1).Printf("This agent is the Kubernetes cluster leader")
	}

	if !agent.IsClusterLeader && previousAgent.IsClusterLeader {
		logger.V(1).Printf("This agent is no longer the Kubernetes cluster leader")
	}

	return nil
}

func (s *Synchronizer) agentsUpdateList(ctx context.Context, execution types.SynchronizationExecution, apiClient types.RawClient) error {
	oldAgents := s.option.Cache.AgentsByUUID()

	params := map[string]string{
		"fields": agentFields,
	}

	result, err := apiClient.Iter(ctx, "agent", params)
	if err != nil {
		return err
	}

	agents := make([]bleemeoTypes.Agent, len(result))

	for i, jsonMessage := range result {
		var agent bleemeoTypes.Agent

		if err := json.Unmarshal(jsonMessage, &agent); err != nil {
			continue
		}

		agents[i] = agent
	}

	s.option.Cache.SetAgentList(agents)

	// If an agent is deleted, ensure our Labels on metric are up-to-date.
	// If an SNMP agent is deleted, its agent UUID is no longer valid and metric
	// should no longer be labeled with it.
	newAgents := s.option.Cache.AgentsByUUID()
	for id := range oldAgents {
		if _, ok := newAgents[id]; !ok {
			execution.RequestNotifyLabelsUpdate()
		}
	}

	return nil
}
