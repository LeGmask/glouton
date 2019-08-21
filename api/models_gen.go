// Code generated by github.com/99designs/gqlgen, DO NOT EDIT.

package api

import (
	"time"
)

type AgentInfo struct {
	RegistrationAt *time.Time `json:"registrationAt"`
	LastReport     *time.Time `json:"lastReport"`
	IsConnected    bool       `json:"isConnected"`
}

type Container struct {
	Command      string     `json:"command"`
	CreatedAt    *time.Time `json:"createdAt"`
	ID           string     `json:"id"`
	Image        string     `json:"image"`
	InspectJSON  string     `json:"inspectJSON"`
	Name         string     `json:"name"`
	StartedAt    *time.Time `json:"startedAt"`
	State        string     `json:"state"`
	FinishedAt   *time.Time `json:"finishedAt"`
	IoWriteBytes float64    `json:"ioWriteBytes"`
	IoReadBytes  float64    `json:"ioReadBytes"`
	NetBitsRecv  float64    `json:"netBitsRecv"`
	NetBitsSent  float64    `json:"netBitsSent"`
	MemUsedPerc  float64    `json:"memUsedPerc"`
	CPUUsedPerc  float64    `json:"cpuUsedPerc"`
}

type Containers struct {
	Count        int          `json:"count"`
	CurrentCount int          `json:"currentCount"`
	Containers   []*Container `json:"containers"`
}

type Fact struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type Label struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type LabelInput struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type Metric struct {
	Name   string   `json:"name"`
	Labels []*Label `json:"labels"`
	Points []*Point `json:"points"`
}

type MetricInput struct {
	Labels []*LabelInput `json:"labels"`
}

type Pagination struct {
	Offset int `json:"offset"`
	Limit  int `json:"limit"`
}

type Point struct {
	Time  time.Time `json:"time"`
	Value float64   `json:"value"`
}

type Process struct {
	Pid         int       `json:"pid"`
	Ppid        int       `json:"ppid"`
	CreateTime  time.Time `json:"create_time"`
	Cmdline     string    `json:"cmdline"`
	Name        string    `json:"name"`
	MemoryRss   int       `json:"memory_rss"`
	CPUPercent  float64   `json:"cpu_percent"`
	CPUTime     float64   `json:"cpu_time"`
	Status      string    `json:"status"`
	Username    string    `json:"username"`
	Executable  string    `json:"executable"`
	ContainerID string    `json:"container_id"`
}

type Service struct {
	Name            string   `json:"name"`
	ContainerID     string   `json:"containerId"`
	IPAddress       string   `json:"ipAddress"`
	ListenAddresses []string `json:"listenAddresses"`
	ExePath         string   `json:"exePath"`
	Active          bool     `json:"active"`
	Status          float64  `json:"status"`
}

type Topinfo struct {
	UpdatedAt time.Time  `json:"updatedAt"`
	Processes []*Process `json:"processes"`
}
