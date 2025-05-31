# Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
# or more contributor license agreements. Licensed under the Elastic License 2.0;
# you may not use this file except in compliance with the Elastic License 2.0.

#
# build go binaries
#
FROM docker.elastic.co/wolfi/go:1.24.3-r1@sha256:18ee0bedf15f2c4cdc7f82ff30f260d9173abc2433680a2debfa69d77b7e804a as builder

ARG VERSION
ARG SHA1
ARG SNAPSHOT

WORKDIR /go/src/github.com/elastic/eck-diagnostics

COPY go.mod go.mod
COPY go.sum go.sum

RUN --mount=type=ssh go mod download

COPY build build
COPY cmd cmd
COPY internal internal
COPY Makefile Makefile

RUN make build-binary generate-notice.txt

#
# package binaries in a lighter final image
#
FROM docker.elastic.co/wolfi/chainguard-base:latest@sha256:4a629c926ff6682fea43cb167bd1eee528667332dbe97afac7d4ae0f591fe4f8

ARG IMAGE_TAG
ENV IMAGE_TAG=$IMAGE_TAG
ARG IMAGE_NAME
ENV IMAGE_NAME=$IMAGE_NAME

RUN apk add --no-cache bash curl

COPY build/scripts /usr/local/bin

COPY --from=builder /go/src/github.com/elastic/eck-diagnostics/artefacts /artefacts
COPY --from=builder /go/src/github.com/elastic/eck-diagnostics/NOTICE.txt /NOTICE.txt

RUN addgroup -S 65536 && adduser -S 65536 -G 65536
USER 65536:65536
WORKDIR /tmp

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
