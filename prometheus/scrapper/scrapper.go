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

package scrapper

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"glouton/logger"
	"glouton/version"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
)

var errIncorrectStatus = errors.New("incorrect status")

// Target is an URL to scrape.
type Target struct {
	URL             *url.URL
	AllowList       []string
	DenyList        []string
	ExtraLabels     map[string]string
	ContainerLabels map[string]string
}

// Gather implement prometheus.Gatherer.
func (t *Target) Gather() ([]*dto.MetricFamily, error) {
	u := t.URL

	logger.V(2).Printf("Scrapping Prometheus exporter %s", u.String())

	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("prepare request to Prometheus exporter %s: %w", u.String(), err)
	}

	req.Header.Add("Accept", "text/plain;version=0.0.4")
	req.Header.Set("User-Agent", version.UserAgent())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := http.DefaultClient.Do(req.WithContext(ctx))
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Ensure response body is read to allow HTTP keep-alive to works
		_, _ = io.Copy(ioutil.Discard, resp.Body)

		return nil, fmt.Errorf("%w: exporter %s HTTP status is %s", errIncorrectStatus, u.String(), resp.Status)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read from %s: %w", u.String(), err)
	}

	reader := bytes.NewReader(body)

	var parser expfmt.TextParser

	resultMap, err := parser.TextToMetricFamilies(reader)
	if err != nil {
		return nil, fmt.Errorf("parse metrics from %s: %w", u.String(), err)
	}

	result := make([]*dto.MetricFamily, 0, len(resultMap))

	for _, family := range resultMap {
		result = append(result, family)
	}

	return result, nil
}
