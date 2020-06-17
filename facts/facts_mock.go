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

package facts

import (
	"context"
	"time"
)

// FactProviderMock provides only hardcoded facts but is useful for testing.
type FactProviderMock struct {
	facts map[string]string
}

// NewMockFacter creates a new lying Fact provider.
func NewMockFacter() *FactProviderMock {
	return &FactProviderMock{
		facts: map[string]string{},
	}
}

// Facts returns the list of facts for this system.
func (f *FactProviderMock) Facts(ctx context.Context, maxAge time.Duration) (facts map[string]string, err error) {
	copy := make(map[string]string, len(f.facts))
	for k, v := range f.facts {
		copy[k] = v
	}

	return copy, nil
}

// SetFact override/add a manual facts
//
// Any fact set using this method is valid until next call to SetFact.
func (f *FactProviderMock) SetFact(key string, value string) {
	f.facts[key] = value
}
