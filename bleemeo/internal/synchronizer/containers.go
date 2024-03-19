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

package synchronizer

import (
	"context"
	"encoding/json"
	"fmt"
	"glouton/bleemeo/client"
	"glouton/bleemeo/internal/common"
	"glouton/bleemeo/internal/synchronizer/types"
	bleemeoTypes "glouton/bleemeo/types"
	"glouton/facts"
	containerTypes "glouton/facts/container-runtime/types"
	"glouton/logger"
	"strings"
	"time"
)

const (
	// containerUpdateDelay is minimal the delay between container update for change
	// other that status (likely healthcheck log).
	containerUpdateDelay = 30 * time.Minute

	// Fields stored in local cache.
	cacheFields = "id,name,container_id,container_inspect,container_status,container_created_at,container_runtime,deleted_at"
	// Fields used to register a container on the API.
	registerFields = cacheFields + ",host,command,container_started_at,container_finished_at,container_image_id,container_image_name,docker_api_version"
)

type containerPayload struct {
	bleemeoTypes.Container
	Host             string                `json:"host"`
	Command          string                `json:"command"`
	StartedAt        bleemeoTypes.NullTime `json:"container_started_at"`
	FinishedAt       bleemeoTypes.NullTime `json:"container_finished_at"`
	ImageID          string                `json:"container_image_id"`
	ImageName        string                `json:"container_image_name"`
	DockerAPIVersion string                `json:"docker_api_version"`
}

func (s *Synchronizer) syncContainers(ctx context.Context, syncType types.SyncType, execution types.SynchronizationExecution) (updateThresholds bool, err error) {
	var localContainers []facts.Container

	cfg, ok := s.option.Cache.CurrentAccountConfig()

	if ok && cfg.DockerIntegration && s.option.Docker != nil {
		var err error

		// We don't need very fresh information, we sync container after discovery which will update containers anyway.
		localContainers, err = s.option.Docker.Containers(ctx, 2*time.Minute, false)
		if err != nil {
			logger.V(1).Printf("Unable to list containers: %v", err)

			return false, nil
		}
	}

	if s.successiveErrors == 3 {
		// After 3 error, try to force a full synchronization to see if it solve the issue.
		syncType = types.SyncTypeForceCacheRefresh
	}

	apiClient := execution.BleemeoAPIClient()

	if syncType == types.SyncTypeForceCacheRefresh {
		err := s.containerUpdateList(ctx, apiClient)
		if err != nil {
			return false, err
		}
	}

	if execution.IsOnlyEssential() {
		// no essential containers, skip registering.
		return false, nil
	}

	// s.containerDeleteFromRemote(): API don't delete containers
	if err := s.containerRegisterAndUpdate(ctx, execution, localContainers); err != nil {
		return false, err
	}

	s.containerDeleteFromLocal(ctx, execution, apiClient, localContainers)

	return false, err
}

func (s *Synchronizer) containerUpdateList(ctx context.Context, apiClient types.RawClient) error {
	params := map[string]string{
		"host":   s.agentID,
		"fields": cacheFields,
	}

	result, err := apiClient.Iter(ctx, "container", params)
	if err != nil {
		return err
	}

	containersByUUID := s.option.Cache.ContainersByUUID()
	containers := make([]bleemeoTypes.Container, 0, len(result))

	for _, jsonMessage := range result {
		var container containerPayload

		if err := json.Unmarshal(jsonMessage, &container); err != nil {
			continue
		}

		// we don't need to keep the full inspect in memory
		container.FillInspectHash()
		container.ContainerInspect = ""
		container.GloutonLastUpdatedAt = containersByUUID[container.ID].GloutonLastUpdatedAt
		containers = append(containers, container.Container)
	}

	s.option.Cache.SetContainers(containers)

	return nil
}

func (s *Synchronizer) containerRegisterAndUpdate(ctx context.Context, execution types.SynchronizationExecution, localContainers []facts.Container) error {
	factsMap, err := s.option.Facts.Facts(ctx, 24*time.Hour)
	if err != nil {
		return err
	}

	remoteContainers := s.option.Cache.Containers()
	remoteIndexByName := make(map[string]int, len(remoteContainers))

	for i, v := range remoteContainers {
		remoteIndexByName[v.Name] = i
	}

	// I'm not sure this really belong here. This update is here because it was here,
	// but this might be better being called by Synchronizer.runOnce()
	execution.GlobalState().UpdateDelayedContainers(localContainers)
	delayedContainer, _ := execution.GlobalState().DelayedContainers()

	for _, container := range localContainers {
		if _, ok := delayedContainer[container.ID()]; ok {
			continue
		}

		name := container.ContainerName()
		if len(name) > common.APIContainerNameLength {
			msg := fmt.Sprintf(
				"Container %s will be ignored because its name is too long (> %d characters)",
				name, common.APIContainerNameLength,
			)

			s.logThrottle(msg)

			continue
		}

		remoteIndex, remoteFound := remoteIndexByName[name]

		var remoteContainer bleemeoTypes.Container

		if remoteFound {
			remoteContainer = remoteContainers[remoteIndex]
		}

		payloadContainer := bleemeoTypes.Container{
			Name:             name,
			ContainerID:      container.ID(),
			ContainerInspect: container.ContainerJSON(),
			Status:           container.State().String(),
			CreatedAt:        container.CreatedAt(),
			Runtime:          container.RuntimeName(),
		}

		payloadContainer.FillInspectHash()

		var remoteDelete bool

		if remoteFound {
			remoteDelete = !time.Time(remoteContainer.DeletedAt).IsZero()
		}

		if remoteFound && payloadContainer.Status == remoteContainer.Status && payloadContainer.CreatedAt.Truncate(time.Second).Equal(remoteContainer.CreatedAt.Truncate(time.Second)) && !remoteDelete {
			if payloadContainer.InspectHash == remoteContainer.InspectHash {
				continue
			}

			if time.Since(remoteContainer.GloutonLastUpdatedAt) < containerUpdateDelay {
				continue
			}
		}

		payloadContainer.InspectHash = ""                   // we don't send inspect hash to API
		payloadContainer.GloutonLastUpdatedAt = time.Time{} // we don't send this time, only used internally
		payload := containerPayload{
			Container:  payloadContainer,
			Host:       s.agentID,
			Command:    strings.Join(container.Command(), " "),
			StartedAt:  bleemeoTypes.NullTime(container.StartedAt()),
			FinishedAt: bleemeoTypes.NullTime(container.FinishedAt()),
			ImageID:    container.ImageID(),
			ImageName:  container.ImageName(),
		}

		if container.RuntimeName() == containerTypes.DockerRuntime {
			payload.DockerAPIVersion = factsMap["docker_api_version"]
		}

		err := s.remoteRegister(ctx, execution.BleemeoAPIClient(), remoteFound, &remoteContainer, &remoteContainers, payload, remoteIndex)
		if err != nil {
			return err
		}
	}

	s.option.Cache.SetContainers(remoteContainers)

	return nil
}

func (s *Synchronizer) remoteRegister(
	ctx context.Context,
	apiClient types.RawClient,
	remoteFound bool,
	remoteContainer *bleemeoTypes.Container,
	remoteContainers *[]bleemeoTypes.Container,
	payload containerPayload,
	remoteIndex int,
) error {
	var result containerPayload

	params := map[string]string{
		"fields": registerFields,
	}

	if remoteFound {
		_, err := apiClient.Do(ctx, "PUT", fmt.Sprintf("v1/container/%s/", remoteContainer.ID), params, payload, &result)
		if err != nil {
			return err
		}

		result.FillInspectHash()
		result.GloutonLastUpdatedAt = time.Now()
		logger.V(2).Printf("Container %v updated with UUID %s", result.Name, result.ID)
		(*remoteContainers)[remoteIndex] = result.Container
	} else {
		_, err := apiClient.Do(ctx, "POST", "v1/container/", params, payload, &result)
		if err != nil {
			return err
		}

		result.FillInspectHash()
		result.GloutonLastUpdatedAt = time.Now()
		logger.V(2).Printf("Container %v registered with UUID %s", result.Name, result.ID)
		*remoteContainers = append(*remoteContainers, result.Container)
	}

	return nil
}

func (s *Synchronizer) containerDeleteFromLocal(ctx context.Context, execution types.SynchronizationExecution, apiClient types.RawClient, localContainers []facts.Container) {
	var deletedIDs []string //nolint: prealloc // we don't know the size. empty is the most likely size.

	duplicatedKey := make(map[string]bool)
	localByContainerID := make(map[string]facts.Container, len(localContainers))

	for _, v := range localContainers {
		localByContainerID[v.ID()] = v
	}

	registeredContainers := s.option.Cache.ContainersByUUID()
	for _, container := range registeredContainers {
		if !time.Time(container.DeletedAt).IsZero() {
			continue
		}

		if _, ok := localByContainerID[container.ContainerID]; ok && !duplicatedKey[container.ContainerID] {
			duplicatedKey[container.ContainerID] = true

			continue
		}

		_, err := apiClient.Do(
			ctx,
			"PATCH",
			fmt.Sprintf("v1/container/%s/", container.ID),
			nil,
			struct {
				DeletedAt bleemeoTypes.NullTime `json:"deleted_at"`
			}{bleemeoTypes.NullTime(s.now())},
			nil,
		)
		if err != nil {
			// If the container was not found it has already been deleted.
			if client.IsNotFound(err) {
				delete(registeredContainers, container.ID)

				deletedIDs = append(deletedIDs, container.ID)
			}

			logger.V(1).Printf("Failed to delete container %v on Bleemeo API: %v", container.Name, err)

			continue
		}

		logger.V(2).Printf("Container %v deleted (UUID %s)", container.Name, container.ID)
		container.DeletedAt = bleemeoTypes.NullTime(s.now())

		registeredContainers[container.ID] = container

		deletedIDs = append(deletedIDs, container.ID)
	}

	containers := make([]bleemeoTypes.Container, 0, len(registeredContainers))

	for _, v := range registeredContainers {
		containers = append(containers, v)
	}

	s.option.Cache.SetContainers(containers)

	if len(deletedIDs) > 0 {
		// API will update all associated metrics and update their active status. Apply the same rule on local cache
		metrics := s.option.Cache.Metrics()
		for i, m := range metrics {
			match := false

			for _, id := range deletedIDs {
				if m.ContainerID == id {
					match = true

					break
				}
			}

			if !match {
				continue
			}

			metrics[i].DeactivatedAt = time.Time(registeredContainers[m.ContainerID].DeletedAt)
		}

		s.option.Cache.SetMetrics(metrics)
		execution.RequestSynchronization(types.EntityService, true)
	}
}
