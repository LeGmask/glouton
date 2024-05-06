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
	"math/rand"
	"testing"
	"time"

	bleemeoTypes "github.com/bleemeo/glouton/bleemeo/types"
)

func Test_applyJitterToMonitorCreationDate(t *testing.T) {
	for range 10 {
		createDate := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(rand.Int63n(int64(365 * 24 * time.Hour)))) //nolint:gosec
		monitor := bleemeoTypes.Monitor{
			Service: bleemeoTypes.Service{
				CreationDate: createDate.Format(time.RFC3339),
			},
		}

		const runCount = 5000

		maxCreateDate := createDate
		minCreateDate := createDate
		countPerTimestamp := make(map[int64]int, runCount)

		wantDelta := 16 * time.Second

		// Due to jitter being randomized, having delta being the full theoretical range would require too
		// much tries. Let's just check that delta is not way too small. This seems
		// enough to avoid flaky test.
		wantDelta -= time.Second

		for range runCount {
			got, err := applyJitterToMonitorCreationDate(monitor, uint64(rand.Int63())) //nolint:gosec
			if err != nil {
				t.Fatal(err)
			}

			// The jitterCreateDate must be within the same minute as original createDate.
			// We want to kept the minute unchanged. This is required for monitor with
			// resolution of 5 minutes because for the quorum Bleemeo assume that the monitor metrics are
			// send at the beginning of the minute after creationDate + N * 5 minutes.
			createDateMinute := createDate.Truncate(time.Minute)
			gotMinute := got.Truncate(time.Minute)

			if !createDateMinute.Equal(gotMinute) {
				t.Fatalf("applyJitterToMonitorCreationDate().Truncate(Minute) = %s, want %s", gotMinute, createDateMinute)
			}

			// The jitterCreateDate must be within the first 45 seconds of the minutes
			// The API task to compute quorum of probes starts at the beginning of every minute,
			// if we run the probe too late in the minute (e.g. 8h20m55s), the new points may
			// not be received by the API on the next quorum (e.g. 8h21m00s). This means the API
			// could use points from the last run (e.g. 8h15m55s), which are more than 5 minutes old.
			// To avoid this problem, we don't run the probes on the last 15 seconds of every minute.
			if got.Second() > 45 {
				t.Fatalf("applyJitterToMonitorCreationDate().Second() = %d, want <= 45", got.Second())
			}

			if got.After(maxCreateDate) {
				maxCreateDate = got
			}

			if got.Before(minCreateDate) {
				minCreateDate = got
			}

			countPerTimestamp[got.Unix()]++
		}

		gotDelta := maxCreateDate.Sub(minCreateDate)

		// applyJitterToMonitorCreationDate should spread creationDate enough
		if gotDelta < wantDelta {
			t.Fatalf("applyJitterToMonitorCreationDate() delta = %s, want >= %s", gotDelta, wantDelta)
		}

		var (
			maxPerBucket int
			minPerBucket int
		)

		for _, count := range countPerTimestamp {
			if maxPerBucket < count {
				maxPerBucket = count
			}

			if minPerBucket == 0 || minPerBucket > count {
				minPerBucket = count
			}
		}

		// applyJitterToMonitorCreationDate should have uniform distribution
		if maxPerBucket-minPerBucket > runCount/10 {
			t.Fatalf("maxPerBucket - minPerBucket = %d, want <= %d\ncountPerTimestamp: %v", maxPerBucket-minPerBucket, runCount/10, countPerTimestamp)
		}
	}
}

func Test_applyJitterToMonitorCreationDateFixedValue(t *testing.T) {
	createDate := time.Date(2024, 4, 30, 16, 32, 47, 123456, time.UTC)
	monitor := bleemeoTypes.Monitor{
		Service: bleemeoTypes.Service{
			CreationDate: createDate.Format(time.RFC3339),
		},
	}

	got, err := applyJitterToMonitorCreationDate(monitor, 42)
	if err != nil {
		t.Fatal(err)
	}

	// What this test check is that wanted value don't change too often, especially between
	// different run and when time.Now() changes.
	// If applyJitterToMonitorCreationDate need to be updated, the wanted value could change.
	want := time.Date(2024, 4, 30, 16, 32, 32, 42000000, time.UTC)

	if !got.Equal(want) {
		t.Errorf("applyJitterToMonitorCreationDate() = %s, want %s", got, want)
	}
}
