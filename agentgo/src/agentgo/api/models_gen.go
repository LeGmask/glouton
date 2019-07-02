// Code generated by github.com/99designs/gqlgen, DO NOT EDIT.

package api

import (
	"time"
)

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

type Label struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type LabelInput struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type LabelsInput struct {
	Labels []*LabelInput `json:"labels"`
}

type Metric struct {
	Name   string   `json:"name"`
	Labels []*Label `json:"labels"`
	Points []*Point `json:"points"`
}

type Pagination struct {
	Offset int `json:"offset"`
	Limit  int `json:"limit"`
}

type Point struct {
	Time  time.Time `json:"time"`
	Value float64   `json:"value"`
}
