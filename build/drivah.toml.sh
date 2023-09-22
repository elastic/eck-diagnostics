#!/usr/bin/env bash

# Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
# or more contributor license agreements. Licensed under the Elastic License 2.0;
# you may not use this file except in compliance with the Elastic License 2.0.

set -eu

ROOT="$(cd "$(dirname "$0")"; pwd)/.."

SNAPSHOT="${SNAPSHOT:-true}"
SHA1="${BUILDKITE_COMMIT:-$SHA1}"
SHA1="${SHA1:0:8}"
VERSION="${BUILDKITE_TAG:-$SHA1}"

IMAGE_NAME="${IMAGE_NAME:-docker.elastic.co/eck/diagnostics}"
IMAGE_TAG="$VERSION"

# relies on BUILDKITE_TAG to disable SNAPSHOT
if [[ "${BUILDKITE_TAG:-}" != "" ]]; then
    SNAPSHOT="false"
fi

cat <<EOF
[container.image]
names = ["${IMAGE_NAME}"]
tags = ["${IMAGE_TAG}"]
build_context = ".."

[container.image.build_args]
IMAGE_NAME = "${IMAGE_NAME}"
IMAGE_TAG = "${IMAGE_TAG}"
VERSION = "${VERSION}"
SHA1 = "${SHA1}"
SNAPSHOT = "${SNAPSHOT}"
EOF
