# Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
# or more contributor license agreements. Licensed under the Elastic License 2.0;
# you may not use this file except in compliance with the Elastic License 2.0.

#
# build go binaries
#
FROM docker.elastic.co/wolfi/go:v1.27.1-r0@sha256:89065a5c117d1d2bdb065ac461d1cd775dff8a4f7e905bfafc4e2c4682738670 as builder

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
FROM docker.elastic.co/wolfi/chainguard-base:latest@sha256:e8b23e9bccb0559f1a5cc3ee7c1e7ba3cd44eab5738897b43d7145f4cea5ce76

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
