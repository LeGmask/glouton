# Glouton Monitoring Agent

![Glouton Logo](logo_glouton.svg)

Glouton have been designed to be a central piece of
a monitoring infrastructure. It gather all information and
send it to... something. We provide drivers for storing in
an InfluxDB database directly or a secure connection (MQTT over SSL) to
Bleemeo Cloud platform.

## Install

If you want to use the Bleemeo Cloud solution see https://docs.bleemeo.com/agent/install-agent/.

## Build a release

Our release version will be set by goreleaser from the current date.

- Glouton have a local UI written in ReactJS. The JS files need to be built before
  the Go binary by running:

```
docker run --rm -u $UID -e HOME=/tmp/home \
   -v $(pwd):/src -w /src/webui \
   node:12.13.0 \
   sh -c 'rm -fr node_modules && npm install && npm run deploy'
```

- Then build the release binaries and Docker image using Goreleaser:

```
docker run --rm -u $UID:999 -e HOME=/go/pkg -e CGO_ENABLED=0 \
   -v $(pwd):/src -w /src \
   -v /var/run/docker.sock:/var/run/docker.sock \
   --entrypoint '' \
   goreleaser/goreleaser sh -c 'go test ./... && goreleaser --rm-dist --snapshot'
```

Release files are present in dist/ folder and a Docker image is build (glouton:latest).

## Run Glouton

On Linux amd64, after building the release you may run it with:

```
./dist/glouton_linux_amd64/glouton
```

Before running the binary, you may want to configure it with:

- (optional) Configure your credentials for Bleemeo Cloud platform:

```
export GLOUTON_BLEEMEO_ACCOUNT_ID=YOUR_ACCOUNT_ID
export GLOUTON_BLEEMEO_REGISTRATION_KEY=YOUR_REGISTRATION_KEY
```

- (optional) If the Bleemeo Cloud platform is running locally:

```
export GLOUTON_BLEEMEO_API_BASE=http://localhost:8000
export GLOUTON_BLEEMEO_MQTT_HOST=localhost
export GLOUTON_BLEEMEO_MQTT_PORT=1883
export GLOUTON_BLEEMEO_MQTT_SSL=False
```


## Test and Develop

Glouton require Golang 1.13. If your system does not provide it, you may run all Go command using Docker.
For example to run test:

```
GOCMD="docker run --net host --rm -ti -v $(pwd):/srv/workspace -w /srv/workspace -u $UID -e HOME=/tmp/home golang go"

$GOCMD test ./...
```

The following will assume "go" is golang 1.13 or more, if not replace it with $GOCMD or use an alias:
```
alias go=$GOCMD
```

Glouton use golangci-lint as linter. You may run it with:
```
mkdir -p /tmp/golangci-lint-cache; docker run --rm -v $(pwd):/app -u $UID -v /tmp/golangci-lint-cache:/go/pkg -e HOME=/go/pkg -w /app golangci/golangci-lint:v1.17.1 golangci-lint run
```

Glouton use Go tests, you may run them with:

```
go test ./... || echo "TEST FAILED"
```

If you updated GraphQL schema or JS files, rebuild JS files (see build a release) and run:

```
go generate glouton/...
```

Then run Glouton from source:

```
go run glouton
```

### Updating dependencies

To update dependencies, you can run:

```
go get -u
```

For some dependencies, you will need to specify the version or commit hash to update to. For example:

```
go get github.com/influxdata/telegraf@1.12.1
```

Running go mod tidy & test before commiting the updated go.mod is recommended:
```
go mod tidy
go test ./...
```

### Note on VS code

Glouton use Go module. VS code support for Go module require usage of gppls.
Enable "Use Language Server" in VS code option for Go.

To install or update gopls, use:

```
(cd /tmp; GO111MODULE=on go get golang.org/x/tools/gopls@latest)
```
