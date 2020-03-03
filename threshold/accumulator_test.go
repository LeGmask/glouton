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

package threshold

import (
	"glouton/types"
	"math"
	"testing"
	"time"
)

func TestStateUpdate(t *testing.T) {
	cases := [][]struct {
		timeOffsetSecond int
		status           types.Status
		want             types.Status
	}{
		{
			{-10, types.StatusOk, types.StatusOk},
			{0, types.StatusWarning, types.StatusOk},
			{10, types.StatusWarning, types.StatusOk},
			{280, types.StatusWarning, types.StatusOk},
			{290, types.StatusWarning, types.StatusOk},
			{300, types.StatusWarning, types.StatusWarning},
		},
		{
			{-10, types.StatusOk, types.StatusOk},
			{0, types.StatusWarning, types.StatusOk},
			{10, types.StatusWarning, types.StatusOk},
			{200, types.StatusCritical, types.StatusOk},
			{290, types.StatusCritical, types.StatusOk},
			{300, types.StatusCritical, types.StatusWarning},
			{310, types.StatusCritical, types.StatusWarning},
			{490, types.StatusCritical, types.StatusWarning},
			{500, types.StatusCritical, types.StatusCritical},
			{510, types.StatusCritical, types.StatusCritical},
			{520, types.StatusWarning, types.StatusWarning},
			{530, types.StatusCritical, types.StatusWarning},
			{820, types.StatusCritical, types.StatusWarning},
			{830, types.StatusCritical, types.StatusCritical},
			{840, types.StatusOk, types.StatusOk},
		},
	}
	now := time.Now()

	for i, c := range cases {
		state := statusState{}

		for _, step := range c {
			state = state.Update(step.status, 300*time.Second, now.Add(time.Duration(step.timeOffsetSecond)*time.Second))
			if state.CurrentStatus != step.want {
				t.Errorf("case #%d offset %d: state.CurrentStatus == %v, want %v", i, step.timeOffsetSecond, state.CurrentStatus, step.want)
				break
			}
		}
	}
}

func TestStateUpdatePeriodChange(t *testing.T) {
	cases := [][]struct {
		period           int
		timeOffsetSecond int
		status           types.Status
		want             types.Status
	}{
		{
			{0, -10, types.StatusOk, types.StatusOk},
			{0, 0, types.StatusWarning, types.StatusWarning},
			{0, 10, types.StatusCritical, types.StatusCritical},
			{300, 90, types.StatusOk, types.StatusOk},
			{300, 100, types.StatusWarning, types.StatusOk},
			{300, 400, types.StatusWarning, types.StatusWarning},
			{500, 410, types.StatusWarning, types.StatusWarning},
		},
	}
	now := time.Now()

	for i, c := range cases {
		state := statusState{}

		for _, step := range c {
			state = state.Update(step.status, time.Duration(step.period)*time.Second, now.Add(time.Duration(step.timeOffsetSecond)*time.Second))
			if state.CurrentStatus != step.want {
				t.Errorf("case #%d offset %d: state.CurrentStatus == %v, want %v", i, step.timeOffsetSecond, state.CurrentStatus, step.want)
				break
			}
		}
	}
}

func TestFormatValue(t *testing.T) {
	cases := []struct {
		value float64
		unit  Unit
		want  string
	}{
		{
			value: 0.,
			unit:  Unit{},
			want:  "0.00",
		},
		{
			value: 0.,
			unit:  Unit{UnitType: UnitTypeUnit, UnitText: "No unit"},
			want:  "0.00",
		},
		{
			value: 0.,
			// 42 is a unknown value for UnitType
			unit: Unit{UnitType: 42, UnitText: "%"},
			want: "0.00 %",
		},
		{
			value: 0.,
			// 42 is a unknown value for UnitType
			unit: Unit{UnitType: 42, UnitText: "thing"},
			want: "0.00 thing",
		},
		{
			value: 0.,
			unit:  Unit{UnitType: UnitTypeByte, UnitText: "Byte"},
			want:  "0.00 Bytes",
		},
		{
			value: 1024,
			unit:  Unit{UnitType: UnitTypeByte, UnitText: "Byte"},
			want:  "1.00 KBytes",
		},
		{
			value: 1 << 30,
			unit:  Unit{UnitType: UnitTypeByte, UnitText: "Byte"},
			want:  "1.00 GBytes",
		},
		{
			value: 1 << 60,
			unit:  Unit{UnitType: UnitTypeByte, UnitText: "Byte"},
			want:  "1.00 EBytes",
		},
		{
			value: 1 << 70,
			unit:  Unit{UnitType: UnitTypeByte, UnitText: "Byte"},
			want:  "1024.00 EBytes",
		},
		{
			value: -1024,
			unit:  Unit{UnitType: UnitTypeByte, UnitText: "Byte"},
			want:  "-1.00 KBytes",
		},
		{
			value: -1 << 30,
			unit:  Unit{UnitType: UnitTypeByte, UnitText: "Byte"},
			want:  "-1.00 GBytes",
		},
	}
	for _, c := range cases {
		got := formatValue(c.value, c.unit)
		if got != c.want {
			t.Errorf("formatValue(%v, %v) == %v, want %v", c.value, c.unit, got, c.want)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		value time.Duration
		want  string
	}{
		{
			value: 300 * time.Second,
			want:  "5 minutes",
		},
		{
			value: 24 * time.Hour,
			want:  "1 day",
		},
		{
			value: 24*time.Hour + 100*time.Second,
			want:  "1 day",
		},
		{
			value: 24*time.Hour - 100*time.Second,
			want:  "1 day",
		},
		{
			value: 89 * time.Second,
			want:  "1 minute",
		},
		{
			value: 91 * time.Second,
			want:  "2 minutes",
		},
		{
			value: 0,
			want:  "",
		},
	}
	for _, c := range cases {
		got := formatDuration(c.value)
		if got != c.want {
			t.Errorf("formatValue(%v) == %v, want %v", c.value, got, c.want)
		}
	}
}

func TestThresholdEqual(t *testing.T) {
	cases := []struct {
		left  Threshold
		right Threshold
		want  bool
	}{
		{
			left:  Threshold{},
			right: Threshold{},
			want:  true,
		},
		{
			left:  Threshold{LowCritical: 1},
			right: Threshold{},
			want:  false,
		},
		{
			left:  Threshold{LowCritical: math.NaN()},
			right: Threshold{},
			want:  false,
		},
		{
			left:  Threshold{LowCritical: math.NaN()},
			right: Threshold{LowCritical: math.NaN()},
			want:  true,
		},
		{
			left:  Threshold{LowCritical: math.NaN(), LowWarning: math.NaN(), HighWarning: math.NaN(), HighCritical: math.NaN()},
			right: Threshold{LowCritical: math.NaN(), LowWarning: math.NaN(), HighWarning: math.NaN(), HighCritical: math.NaN()},
			want:  true,
		},
		{
			left:  Threshold{LowCritical: 5, LowWarning: math.NaN(), HighWarning: math.NaN(), HighCritical: math.NaN()},
			right: Threshold{LowCritical: 5, LowWarning: math.NaN(), HighWarning: math.NaN(), HighCritical: math.NaN()},
			want:  true,
		},
		{
			left:  Threshold{LowCritical: 5, LowWarning: math.NaN(), HighWarning: math.NaN(), HighCritical: math.NaN()},
			right: Threshold{LowCritical: 6, LowWarning: math.NaN(), HighWarning: math.NaN(), HighCritical: math.NaN()},
			want:  false,
		},
	}
	for i, c := range cases {
		got := c.left.Equal(c.right)
		if got != c.want {
			t.Errorf("case %d: left.Equal(right) == %v, want %v", i, got, c.want)
		}

		got = c.right.Equal(c.left)
		if got != c.want {
			t.Errorf("case %d: right.Equal(left) == %v, want %v", i, got, c.want)
		}
	}
}
