SHELL := /bin/bash
export VERSION ?= v1.0.0
export GOBIN = $(shell pwd)/bin

BINARY := eck-diagnostics

all: bin/eck-diagnostics NOTICE.txt

# build
bin/eck-diagnostics: lint
	@ GCO_ENABLED=0 go build -o $(GOBIN)/eck-diagnostics github.com/elastic/eck-diagnostics/cmd

NOTICE.txt: $(GOBIN)/go-licence-detector
	@ go list -m -json all | $(GOBIN)/go-licence-detector \
 		-noticeTemplate=build/notice/NOTICE.txt.tpl \
 		-noticeOut=NOTICE.txt \
 		-overrides=build/notice/overrides.json \
 		-includeIndirect

.PHONY: lint
lint: $(GOBIN)/golangci-lint
	@ $(GOBIN)/golangci-lint run

# tool dependencies
$(GOBIN)/go-licence-detector:
	@ go install go.elastic.co/go-licence-detector@v0.3.0

$(GOBIN)/golangci-lint:
	@ go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.41.1