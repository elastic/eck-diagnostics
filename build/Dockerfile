# Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
# or more contributor license agreements. Licensed under the Elastic License 2.0;
# you may not use this file except in compliance with the Elastic License 2.0.

#
# build binaries using golang image
#
FROM golang:1.20.6 as builder

ARG VERSION
ARG SHA1
ARG SNAPSHOT

WORKDIR /go/src/github.com/elastic/eck-diagnostics
COPY . ./

RUN ls
RUN make build-binary

#
# copy binaries in light final image
#
FROM alpine:3.18

COPY --from=builder /go/src/github.com/elastic/eck-diagnostics/NOTICE.txt /NOTICE.txt
COPY --from=builder /go/src/github.com/elastic/eck-diagnostics/bin/eck-* /artefacts

ENTRYPOINT ["ls", "-1", "/artefacts"]
