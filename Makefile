# Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
# or more contributor license agreements. Licensed under the Elastic License 2.0;
# you may not use this file except in compliance with the Elastic License 2.0.

SHELL := /bin/bash
export VERSION ?= 1.1.0
export GOBIN = $(shell pwd)/bin

SNAPSHOT  ?= true
SHA1      ?= $(shell git rev-parse --short=8 --verify HEAD)

LDFLAGS ?= -X github.com/elastic/eck-diagnostics/internal.buildVersion=$(VERSION) \
	-X github.com/elastic/eck-diagnostics/internal.buildHash=$(SHA1) \
	-X github.com/elastic/eck-diagnostics/internal.buildDate=$(shell date -u +'%Y-%m-%dT%H:%M:%SZ')) \
	-X github.com/elastic/eck-diagnostics/internal.snapshotBuild=$(SNAPSHOT)

all: $(GOBIN)/eck-diagnostics NOTICE.txt

# build
$(GOBIN)/eck-diagnostics:
	@ GCO_ENABLED=0 go build \
		-mod=readonly \
		-ldflags="$(LDFLAGS)" github.com/elastic/eck-diagnostics/cmd \
		-o $(GOBIN)/eck-diagnostics

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
	@ go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.53.2
