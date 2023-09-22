# Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
# or more contributor license agreements. Licensed under the Elastic License 2.0;
# you may not use this file except in compliance with the Elastic License 2.0.

SHELL := /bin/bash
export GOBIN 	 = $(shell pwd)/bin

export SNAPSHOT ?= true
export SHA1     ?= $(shell git rev-parse --short=8 --verify HEAD)
export VERSION  ?= $(SHA1)

GO_LDFLAGS := -X github.com/elastic/eck-diagnostics/internal.buildVersion=$(VERSION) \
	-X github.com/elastic/eck-diagnostics/internal.buildHash=$(SHA1) \
	-X github.com/elastic/eck-diagnostics/internal.buildDate=$(shell date -u +'%Y-%m-%dT%H:%M:%SZ') \
	-X github.com/elastic/eck-diagnostics/internal.snapshotBuild=$(SNAPSHOT)

lint: $(GOBIN)/golangci-lint
	$(GOBIN)/golangci-lint run

unit-tests:
	go test -v ./...

GOOS   ?= darwin linux windows
GOARCH ?= amd64 arm64

build-binary:
	@ for os in $(GOOS); do \
		for arch in $(GOARCH); do \
			echo "-- build artefacts/eck-diagnostics-$${os}-$${arch}"; \
			GCO_ENABLED=0 GOOS=$${os} GOARCH=$${arch} go build \
				-mod readonly \
				-ldflags "$(GO_LDFLAGS)" -a \
				-o artefacts/eck-diagnostics-$${os}-$${arch} \
				github.com/elastic/eck-diagnostics/cmd \
			; \
		done \
	done

build-image:
	drivah build .

build-push-image:
	drivah build . --push

generate-notice.txt: $(GOBIN)/go-licence-detector
	@ go list -m -json all | $(GOBIN)/go-licence-detector \
 		-noticeTemplate=build/notice/NOTICE.txt.tpl \
 		-noticeOut=NOTICE.txt \
 		-overrides=build/notice/overrides.json \
 		-includeIndirect

# tool dependencies

$(GOBIN)/go-licence-detector:
	@ go install go.elastic.co/go-licence-detector@v0.3.0

$(GOBIN)/golangci-lint:
	@ go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.53.2
