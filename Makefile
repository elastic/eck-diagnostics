SHELL := /bin/bash
export VERSION ?= 1.1.0
export GOBIN = $(shell pwd)/bin

SNAPSHOT ?= true
TAG ?= $(shell git show --format='%h' HEAD --quiet)
LDFLAGS ?= -X github.com/elastic/eck-diagnostics/internal.buildVersion=$(VERSION) \
	-X github.com/elastic/eck-diagnostics/internal.buildHash=$(TAG) \
	-X github.com/elastic/eck-diagnostics/internal.buildDate=$(shell date -u +'%Y-%m-%dT%H:%M:%SZ')) \
	-X github.com/elastic/eck-diagnostics/internal.snapshotBuild=$(SNAPSHOT)

all: $(GOBIN)/eck-diagnostics NOTICE.txt

# build
$(GOBIN)/eck-diagnostics: unit
	@ GCO_ENABLED=0 go build -o $(GOBIN)/eck-diagnostics -ldflags="$(LDFLAGS)" github.com/elastic/eck-diagnostics/cmd

NOTICE.txt: $(GOBIN)/go-licence-detector
	@ go list -m -json all | $(GOBIN)/go-licence-detector \
 		-noticeTemplate=build/notice/NOTICE.txt.tpl \
 		-noticeOut=NOTICE.txt \
 		-overrides=build/notice/overrides.json \
 		-includeIndirect

.PHONY: unit
unit: lint
	@ go test -v ./...

.PHONY: lint
lint: $(GOBIN)/golangci-lint
	@ $(GOBIN)/golangci-lint run

# tool dependencies
$(GOBIN)/go-licence-detector:
	@ go install go.elastic.co/go-licence-detector@v0.3.0

$(GOBIN)/golangci-lint:
	@ go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.48.0
