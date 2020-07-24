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

package api

//go:generate go run github.com/markbates/pkger/cmd/pkger

import (
	"context"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"net/http"
	"time"

	"glouton/discovery"
	"glouton/facts"
	"glouton/logger"
	"glouton/threshold"
	"glouton/types"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/playground"
	"github.com/go-chi/chi"
	"github.com/markbates/pkger"
	"github.com/rs/cors"
)

type storeInterface interface {
	Metrics(filters map[string]string) (result []types.Metric, err error)
}

type dockerInterface interface {
	Containers(ctx context.Context, maxAge time.Duration, includeIgnored bool) (containers []facts.Container, err error)
}

type agentInterface interface {
	BleemeoRegistrationAt() time.Time
	BleemeoLastReport() time.Time
	BleemeoConnected() bool
	Tags() []string
}

// API contains API's port.
type API struct {
	BindAddress        string
	StaticCDNURL       string
	DB                 storeInterface
	DockerFact         dockerInterface
	PsFact             *facts.ProcessProvider
	FactProvider       *facts.FactProvider
	Disccovery         *discovery.Discovery
	AgentInfo          agentInterface
	PrometheurExporter http.Handler
	Threshold          *threshold.Registry
	DiagnosticPage     func() string
	DiagnosticZip      func(w io.Writer) error

	router http.Handler
}

type gloutonUIConfig struct {
	StaticCDNURL string
}

func (api *API) init() {
	router := chi.NewRouter()
	router.Use(cors.New(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowCredentials: true,
		Debug:            false,
	}).Handler)

	_ = pkger.Include("/api/static")
	staticFolder := pkger.Dir("/api/static")
	assetsFolder := pkger.Dir("/api/static/assets")

	fallbackIndex := []byte("Error while initializing local UI. See Glouton logs")

	var indexBody []byte

	indexFile, err := staticFolder.Open("/index.html")
	if err == nil {
		indexBody, err = ioutil.ReadAll(indexFile)
	}

	if err != nil {
		logger.Printf("Error while loading index.html. Local UI will be broken: %v", err)

		indexBody = fallbackIndex
	}

	indexTmpl, err := template.New("index").Parse(string(indexBody))
	if err != nil {
		logger.Printf("Error while loading index.html. Local UI will be broken: %v", err)
	}

	var diagnosticTmpl *template.Template

	var diagnosticBody []byte

	diagnosticFile, err := staticFolder.Open("/diagnostic.html")
	if err == nil {
		diagnosticBody, err = ioutil.ReadAll(diagnosticFile)
	}

	if err != nil {
		logger.Printf("Error while loading diagnostic.html: %v", err)
	} else {
		diagnosticTmpl, err = template.New("diagnostic").Parse(string(diagnosticBody))
		if err != nil {
			diagnosticTmpl = nil
			logger.Printf("Error while loading diagnostic.html. Local UI will be broken: %v", err)
		}
	}

	router.Handle("/metrics", api.PrometheurExporter)
	router.Handle("/playground", playground.Handler("GraphQL playground", "/graphql"))
	router.Handle("/graphql", handler.NewDefaultServer(NewExecutableSchema(Config{Resolvers: &Resolver{api: api}})))
	router.HandleFunc("/diagnostic", func(w http.ResponseWriter, r *http.Request) {
		content := api.DiagnosticPage()

		var err error

		if diagnosticTmpl == nil {
			fmt.Fprintln(w, "diagnostic.html load failed. Fallback to simple text")
			_, err = w.Write([]byte(content))
		} else {
			err = diagnosticTmpl.Execute(w, content)
		}

		if err != nil {
			logger.V(2).Printf("failed to serve index.html: %v", err)
		}
	})

	router.HandleFunc("/diagnostic.zip", func(w http.ResponseWriter, r *http.Request) {
		hdr := w.Header()
		hdr.Add("Content-Type", "application/zip")

		if err := api.DiagnosticZip(w); err != nil {
			logger.V(1).Printf("failed to serve diagnostic.zip: %v", err)
		}
	})

	router.Handle("/static/*", http.StripPrefix("/static", http.FileServer(assetsFolder)))
	router.HandleFunc("/*", func(w http.ResponseWriter, r *http.Request) {
		var err error
		if indexTmpl == nil {
			_, err = w.Write(fallbackIndex)
		} else {
			err = indexTmpl.Execute(w, gloutonUIConfig{
				StaticCDNURL: api.StaticCDNURL,
			})
		}
		if err != nil {
			logger.V(2).Printf("fail to serve index.html: %v", err)
		}
	})

	api.router = router
}

// Run starts our API.
func (api *API) Run(ctx context.Context) error {
	api.init()

	srv := http.Server{
		Addr:    api.BindAddress,
		Handler: api.router,
	}

	idleConnsClosed := make(chan struct{})

	go func() {
		<-ctx.Done()

		subCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := srv.Shutdown(subCtx); err != nil {
			logger.V(2).Printf("HTTP server Shutdown: %v", err)
		}

		close(idleConnsClosed)
	}()

	logger.Printf("Starting API on %s ✔️", api.BindAddress)
	logger.Printf("To access the local panel connect to http://%s 🌐", api.BindAddress)

	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}

	return nil
}
