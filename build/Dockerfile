# Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
# or more contributor license agreements. Licensed under the Elastic License 2.0;
# you may not use this file except in compliance with the Elastic License 2.0.

#
# build go binaries
#
FROM docker.elastic.co/wolfi/go:1.25.7-r0@sha256:8a7294a5dc50238f50c95ba98db49a41a1a3b54150c4a6a9de35b89b17a6480e as builder

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
FROM docker.elastic.co/wolfi/chainguard-base:latest@sha256:a71dfa7c4cb17fb0f6814cda77e0bdf7b66dec9b6725fe61abd29c2263775af7

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
