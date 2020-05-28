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

// nolint: scopelint
package facts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	containerTypes "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"
)

// testdata could be generated by running "docker inspect name1 name2 ... > testdata/docker-VERSION.json"
// Some file use older version of Docker, which could be done with:
// docker-machine create --virtualbox-boot2docker-url https://github.com/boot2docker/boot2docker/releases/download/v19.03.5/boot2docker.iso machine-name
//
// The following container are used
// docker run -d --name noport busybox sleep 99d
// docker run -d --name my_nginx -p 8080:80 nginx
// docker run -d --name my_redis redis
// docker run -d --name multiple-port -p 5672:5672 rabbitmq
// docker run -d --name multiple-port2 rabbitmq
// docker run -d --name non-standard-port -p 4242:4343 -p 1234:1234 rabbitmq
//
// Other container using docker-compose are used (see docker-compose.yaml in testdata folder)

type mockDockerClient struct {
	containers []types.ContainerJSON
}

func newDockerMock(filename string) (mockDockerClient, error) {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return mockDockerClient{}, err
	}

	result := mockDockerClient{}

	err = json.Unmarshal(data, &result.containers)
	if err != nil {
		return result, err
	}

	return result, err
}

func (cl mockDockerClient) ContainerExecAttach(ctx context.Context, execID string, config types.ExecStartCheck) (types.HijackedResponse, error) {
	return types.HijackedResponse{}, errors.New("ContainerExecAttach not implemented")
}
func (cl mockDockerClient) ContainerExecCreate(ctx context.Context, container string, config types.ExecConfig) (types.IDResponse, error) {
	return types.IDResponse{}, errors.New("ContainerExecCreatenot implemented")
}
func (cl mockDockerClient) ContainerInspect(ctx context.Context, container string) (types.ContainerJSON, error) {
	for _, c := range cl.containers {
		if c.ID == container || c.Name == "/"+container {
			return c, nil
		}
	}

	return types.ContainerJSON{}, errors.New("not found?")
}
func (cl mockDockerClient) ContainerList(ctx context.Context, options types.ContainerListOptions) ([]types.Container, error) {
	if !reflect.DeepEqual(options, types.ContainerListOptions{All: true}) {
		return nil, errors.New("ContainerList not implemented with options other that all=True")
	}

	result := make([]types.Container, len(cl.containers))
	for i, c := range cl.containers {
		result[i] = types.Container{
			ID: c.ID,
		}
	}

	return result, nil
}
func (cl mockDockerClient) ContainerTop(ctx context.Context, container string, arguments []string) (containerTypes.ContainerTopOKBody, error) {
	return containerTypes.ContainerTopOKBody{}, errors.New("ContainerTop not implemented")
}
func (cl mockDockerClient) Events(ctx context.Context, options types.EventsOptions) (<-chan events.Message, <-chan error) {
	return nil, nil
}
func (cl mockDockerClient) NetworkInspect(ctx context.Context, network string, options types.NetworkInspectOptions) (types.NetworkResource, error) {
	return types.NetworkResource{}, errors.New("NetworkInspect not implemented")
}
func (cl mockDockerClient) NetworkList(ctx context.Context, options types.NetworkListOptions) ([]types.NetworkResource, error) {
	return nil, errors.New("NetworkList not implemented")
}
func (cl mockDockerClient) Ping(ctx context.Context) (types.Ping, error) {
	return types.Ping{}, nil
}
func (cl mockDockerClient) ServerVersion(ctx context.Context) (types.Version, error) {
	return types.Version{}, errors.New("ServerVersion not implemented")
}

func (cl mockDockerClient) getContainer(container string) types.ContainerJSON {
	var candidate types.ContainerJSON

	for _, c := range cl.containers {
		if c.Name == "/"+container {
			return c
		}

		if strings.Contains(c.Name, container) {
			if candidate.ContainerJSONBase != nil {
				panic(fmt.Sprintf("%#v: two candidate", container))
			}

			candidate = c
		}
	}

	if candidate.ContainerJSONBase == nil {
		panic(fmt.Sprintf("%#v: not found", container))
	}

	return candidate
}

type mockKubernetesClient struct {
	list corev1.PodList
}

func newKubernetesMock(filename string) (mockKubernetesClient, error) {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return mockKubernetesClient{}, err
	}

	result := mockKubernetesClient{}

	err = yaml.Unmarshal(data, &result.list)
	if err != nil {
		return result, err
	}

	return result, err
}

func (cl mockKubernetesClient) PODs(ctx context.Context, maxAge time.Duration) ([]corev1.Pod, error) {
	return cl.list.Items, nil
}
func (cl mockKubernetesClient) getPOD(name string) corev1.Pod {
	var candidate corev1.Pod

	for _, p := range cl.list.Items {
		if p.Name == name {
			return p
		}

		if strings.Contains(p.Name, name) {
			if candidate.Name != "" {
				panic(fmt.Sprintf("%#v: two candidate", name))
			}

			candidate = p
		}
	}

	if candidate.Name == "" {
		panic(fmt.Sprintf("%#v: not found", name))
	}

	return candidate
}

func TestContainer_ListenAddresses(t *testing.T) {
	type fields struct {
		primaryAddress string
	}

	docker1_13_1, err := newDockerMock("testdata/docker-v1.13.1.json")
	if err != nil {
		t.Fatal(err)
	}

	docker17_06, err := newDockerMock("testdata/docker-v17.06.0-ce.json")
	if err != nil {
		t.Fatal(err)
	}

	docker18_09, err := newDockerMock("testdata/docker-v18.09.4.json")
	if err != nil {
		t.Fatal(err)
	}

	docker19_03, err := newDockerMock("testdata/docker-v19.03.5.json")
	if err != nil {
		t.Fatal(err)
	}

	dockerMinikube1_18, err := newDockerMock("testdata/minikube-v1.18.0/docker.json")
	if err != nil {
		t.Fatal(err)
	}

	kubernetes1_18, err := newKubernetesMock("testdata/minikube-v1.18.0/pods.yaml")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name            string
		dockerContainer types.ContainerJSON
		k8sPod          corev1.Pod
		fields          fields
		want            []ListenAddress
	}{
		{
			name:            "docker-noport",
			dockerContainer: docker19_03.getContainer("noport"),
			fields: fields{
				primaryAddress: "10.0.0.42",
			},
			want: []ListenAddress{},
		},
		{
			name:            "docker-oneport",
			dockerContainer: docker19_03.getContainer("my_redis"),
			fields: fields{
				primaryAddress: "10.0.0.42",
			},
			want: []ListenAddress{
				{Address: "10.0.0.42", NetworkFamily: "tcp", Port: 6379},
			},
		},
		{
			name:            "docker-oneport-exposed",
			dockerContainer: docker19_03.getContainer("my_nginx"),
			fields: fields{
				primaryAddress: "10.0.0.42",
			},
			want: []ListenAddress{
				{Address: "10.0.0.42", NetworkFamily: "tcp", Port: 80},
			},
		},
		{
			name:            "docker-multiple-port-one-exposed",
			dockerContainer: docker19_03.getContainer("multiple-port"),
			fields: fields{
				primaryAddress: "10.0.0.42",
			},
			want: []ListenAddress{
				{Address: "10.0.0.42", NetworkFamily: "tcp", Port: 5672},
			},
		},
		{
			name:            "docker-multiple-port-none-exposed",
			dockerContainer: docker19_03.getContainer("multiple-port2"),
			fields: fields{
				primaryAddress: "10.0.0.42",
			},
			want: []ListenAddress{
				{Address: "10.0.0.42", NetworkFamily: "tcp", Port: 4369},
				{Address: "10.0.0.42", NetworkFamily: "tcp", Port: 5671},
				{Address: "10.0.0.42", NetworkFamily: "tcp", Port: 5672},
				{Address: "10.0.0.42", NetworkFamily: "tcp", Port: 25672},
			},
		},
		{
			name:            "docker-multiple-port-other-exposed",
			dockerContainer: docker19_03.getContainer("non-standard-port"),
			fields: fields{
				primaryAddress: "10.0.0.42",
			},
			want: []ListenAddress{
				{Address: "10.0.0.42", NetworkFamily: "tcp", Port: 1234},
				{Address: "10.0.0.42", NetworkFamily: "tcp", Port: 4343},
			},
		},
		{
			name:            "docker-v18.09.4",
			dockerContainer: docker18_09.getContainer("my_redis"),
			fields: fields{
				primaryAddress: "10.0.0.42",
			},
			want: []ListenAddress{
				{Address: "10.0.0.42", NetworkFamily: "tcp", Port: 6379},
			},
		},
		{
			name:            "docker-v17.06.0",
			dockerContainer: docker17_06.getContainer("my_nginx"),
			fields: fields{
				primaryAddress: "10.0.0.42",
			},
			want: []ListenAddress{
				{Address: "10.0.0.42", NetworkFamily: "tcp", Port: 80},
			},
		},
		{
			name:            "docker-v1.13.1",
			dockerContainer: docker1_13_1.getContainer("multiple-port"),
			fields: fields{
				primaryAddress: "10.0.0.42",
			},
			want: []ListenAddress{
				{Address: "10.0.0.42", NetworkFamily: "tcp", Port: 5672},
			},
		},
		{
			name:            "docker-compose-one-exposed",
			dockerContainer: docker19_03.getContainer("testdata_rabbitmqExposed_1"),
			fields: fields{
				primaryAddress: "10.0.0.42",
			},
			want: []ListenAddress{
				{Address: "10.0.0.42", NetworkFamily: "tcp", Port: 5671},
			},
		},
		{
			name:            "docker-compose-none-exposed",
			dockerContainer: docker19_03.getContainer("testdata_rabbitmqInternal_1"),
			fields: fields{
				primaryAddress: "10.0.0.42",
			},
			want: []ListenAddress{
				{Address: "10.0.0.42", NetworkFamily: "tcp", Port: 4369},
				{Address: "10.0.0.42", NetworkFamily: "tcp", Port: 5671},
				{Address: "10.0.0.42", NetworkFamily: "tcp", Port: 5672},
				{Address: "10.0.0.42", NetworkFamily: "tcp", Port: 25672},
			},
		},
		{
			name:            "docker-compose-labels",
			dockerContainer: docker19_03.getContainer("testdata_rabbitLabels_1"),
			fields: fields{
				primaryAddress: "10.0.0.42",
			},
			want: []ListenAddress{
				{Address: "10.0.0.42", NetworkFamily: "tcp", Port: 4369},
				{Address: "10.0.0.42", NetworkFamily: "tcp", Port: 5671},
				{Address: "10.0.0.42", NetworkFamily: "tcp", Port: 5672},
				{Address: "10.0.0.42", NetworkFamily: "tcp", Port: 25672},
			},
		},
		{
			name:            "minikube-no-k8s-api",
			dockerContainer: dockerMinikube1_18.getContainer("rabbitmq_rabbitmq-container-port"),
			fields: fields{
				primaryAddress: "10.0.0.42",
			},
			want: []ListenAddress{
				{Address: "10.0.0.42", NetworkFamily: "tcp", Port: 4369},
				{Address: "10.0.0.42", NetworkFamily: "tcp", Port: 5671},
				{Address: "10.0.0.42", NetworkFamily: "tcp", Port: 5672},
				{Address: "10.0.0.42", NetworkFamily: "tcp", Port: 25672},
			},
		},
		{
			name:            "minikube-container-port",
			dockerContainer: dockerMinikube1_18.getContainer("rabbitmq_rabbitmq-container-port"),
			k8sPod:          kubernetes1_18.getPOD("rabbitmq-container-port"),
			fields: fields{
				primaryAddress: "10.0.0.42",
			},
			want: []ListenAddress{
				{Address: "10.0.0.42", NetworkFamily: "tcp", Port: 5672},
			},
		},
		{
			name:            "minikube-labels",
			dockerContainer: dockerMinikube1_18.getContainer("rabbitmq_rabbitmq-labels"),
			k8sPod:          kubernetes1_18.getPOD("rabbitmq-labels"),
			fields: fields{
				primaryAddress: "10.0.0.42",
			},
			want: []ListenAddress{
				{Address: "10.0.0.42", NetworkFamily: "tcp", Port: 4369},
				{Address: "10.0.0.42", NetworkFamily: "tcp", Port: 5671},
				{Address: "10.0.0.42", NetworkFamily: "tcp", Port: 5672},
				{Address: "10.0.0.42", NetworkFamily: "tcp", Port: 25672},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Container{
				primaryAddress: tt.fields.primaryAddress,
				inspect:        tt.dockerContainer,
				pod:            tt.k8sPod,
			}

			if got := c.ListenAddresses(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Container.ListenAddresses() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestRegress_withoutKubernetes verify that Docker provider can work without Kubernetes
func TestRegress_withoutKubernetes(t *testing.T) {
	dockerClient, err := newDockerMock("testdata/minikube-v1.18.0/docker.json")
	if err != nil {
		t.Fatal(err)
	}

	dockerProvider := NewDocker(nil, nil)
	dockerProvider.client = dockerClient

	_, err = dockerProvider.Containers(context.Background(), 0, true)
	if err != nil {
		t.Error(err)
	}

	var kubeProvider *KubernetesProvider

	dockerProvider = NewDocker(nil, kubeProvider)
	dockerProvider.client = dockerClient

	_, err = dockerProvider.Containers(context.Background(), 0, true)
	if err != nil {
		t.Error(err)
	}
}

func Test_updateContainers(t *testing.T) {
	dockerClient, err := newDockerMock("testdata/minikube-v1.18.0/docker.json")
	if err != nil {
		t.Fatal(err)
	}

	kubernetesClient, err := newKubernetesMock("testdata/minikube-v1.18.0/pods.yaml")
	if err != nil {
		t.Fatal(err)
	}

	dockerProvider := DockerProvider{
		client:             dockerClient,
		kubernetesProvider: kubernetesClient,
	}

	containers, err := dockerProvider.Containers(context.Background(), 0, true)
	if err != nil {
		t.Error(err)
	}

	want := []struct {
		containerNameContains string
		ignored               bool
		primaryAddress        string
		listenAddress         []ListenAddress
		ignoredPorts          map[int]bool
	}{
		{
			containerNameContains: "rabbitmq_rabbitmq-container-port",
			ignored:               false,
			primaryAddress:        "172.18.0.7",
			listenAddress: []ListenAddress{
				{Address: "172.18.0.7", NetworkFamily: "tcp", Port: 5672},
			},
			ignoredPorts: map[int]bool{},
		},
		{
			containerNameContains: "rabbitmq_rabbitmq-labels",
			ignored:               false,
			primaryAddress:        "172.18.0.6",
			listenAddress: []ListenAddress{
				{Address: "172.18.0.6", NetworkFamily: "tcp", Port: 4369},
				{Address: "172.18.0.6", NetworkFamily: "tcp", Port: 5671},
				{Address: "172.18.0.6", NetworkFamily: "tcp", Port: 5672},
				{Address: "172.18.0.6", NetworkFamily: "tcp", Port: 25672},
			},
			ignoredPorts: map[int]bool{
				4369: true,
				5671: true,
			},
		},
		{
			containerNameContains: "the-redis_redis-memcached",
			ignored:               true,
			primaryAddress:        "172.18.0.5",
			listenAddress: []ListenAddress{
				{Address: "172.18.0.5", NetworkFamily: "tcp", Port: 6363},
			},
			ignoredPorts: map[int]bool{},
		},
		{
			containerNameContains: "a-memcached_redis-memcached",
			ignored:               true,
			primaryAddress:        "172.18.0.5",
			listenAddress: []ListenAddress{
				{Address: "172.18.0.5", NetworkFamily: "tcp", Port: 11211},
			},
			ignoredPorts: map[int]bool{},
		},
	}

	if len(containers) != 7 {
		t.Errorf("len(containers) = %d, want 7", len(containers))
	}

	for _, w := range want {
		found := false

		for _, c := range containers {
			if !strings.Contains(c.Name(), w.containerNameContains) {
				continue
			}

			if found {
				t.Errorf("two or more containers contains %#v", w.containerNameContains)
			}

			found = true

			if c.Ignored() != w.ignored {
				t.Errorf("c.Ignored() = %v, want %v", c.Ignored(), w.ignored)
			}

			if c.PrimaryAddress() != w.primaryAddress {
				t.Errorf("c.PrimaryAddress() = %v, want %v", c.PrimaryAddress(), w.primaryAddress)
			}

			if w.listenAddress != nil && !reflect.DeepEqual(c.ListenAddresses(), w.listenAddress) {
				t.Errorf("c.ListenAddresses() = %v, want %v", c.ListenAddresses(), w.listenAddress)
			}

			if w.ignoredPorts != nil && !reflect.DeepEqual(c.IgnoredPorts(), w.ignoredPorts) {
				t.Errorf("c.IgnoredPorts() = %v, want %v", c.IgnoredPorts(), w.ignoredPorts)
			}
		}

		if !found {
			t.Errorf("no contains has the name %#v", w.containerNameContains)
		}
	}
}

func TestContainer_IgnoredPorts(t *testing.T) {
	docker1_13_1, err := newDockerMock("testdata/docker-v1.13.1.json")
	if err != nil {
		t.Fatal(err)
	}

	docker19_03, err := newDockerMock("testdata/docker-v19.03.5.json")
	if err != nil {
		t.Fatal(err)
	}

	dockerMinikube1_18, err := newDockerMock("testdata/minikube-v1.18.0/docker.json")
	if err != nil {
		t.Fatal(err)
	}

	kubernetes1_18, err := newKubernetesMock("testdata/minikube-v1.18.0/pods.yaml")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name            string
		dockerContainer types.ContainerJSON
		k8sPod          corev1.Pod
		want            map[int]bool
	}{
		{
			name:            "docker-noport",
			dockerContainer: docker19_03.getContainer("noport"),
			want:            map[int]bool{},
		},
		{
			name:            "docker-oneport",
			dockerContainer: docker19_03.getContainer("my_redis"),
			want:            map[int]bool{},
		},
		{
			name:            "docker-v1.13.1",
			dockerContainer: docker1_13_1.getContainer("multiple-port"),
			want:            map[int]bool{},
		},
		{
			name:            "docker-compose-none-exposed",
			dockerContainer: docker19_03.getContainer("testdata_rabbitmqInternal_1"),
			want:            map[int]bool{},
		},
		{
			name:            "docker-compose-labels",
			dockerContainer: docker19_03.getContainer("testdata_rabbitLabels_1"),
			want: map[int]bool{
				4369:  true,
				5672:  false,
				25672: true,
			},
		},
		{
			name:            "minikube-no-k8s-api",
			dockerContainer: dockerMinikube1_18.getContainer("rabbitmq_rabbitmq-container-port"),
			want:            map[int]bool{},
		},
		{
			name:            "minikube-container-port",
			dockerContainer: dockerMinikube1_18.getContainer("rabbitmq_rabbitmq-container-port"),
			k8sPod:          kubernetes1_18.getPOD("rabbitmq-container-port"),
			want:            map[int]bool{},
		},
		{
			name:            "minikube-labels",
			dockerContainer: dockerMinikube1_18.getContainer("rabbitmq_rabbitmq-labels"),
			k8sPod:          kubernetes1_18.getPOD("rabbitmq-labels"),
			want: map[int]bool{
				5671: true,
				4369: true,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Container{
				inspect: tt.dockerContainer,
				pod:     tt.k8sPod,
			}

			if got := c.IgnoredPorts(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Container.IgnoredPorts() = %v, want %v", got, tt.want)
			}
		})
	}
}
