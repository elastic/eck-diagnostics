#!/usr/bin/env bash

# Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
# or more contributor license agreements. Licensed under the Elastic License 2.0;
# you may not use this file except in compliance with the Elastic License 2.0.

set -eu

ROOT="$(cd "$(dirname "$0")"; pwd)/.."

SNAPSHOT="${SNAPSHOT:-true}"
SHA1="${BUILDKITE_COMMIT:-$SHA1}"
VERSION="${VERSION:-$(cat "$ROOT/VERSION")}"

tag() {
    tag="$VERSION-SNAPSHOT"
    if [[ "${BUILDKITE_TAG:-}" != "" ]]; then
        SNAPSHOT=""
        tag="$VERSION"
    fi
    echo "$tag"
}

cat <<EOF
[container.image]
names = ["docker.elastic.co/eck/eck-diagnostics"]
tags = ["latest", "$SHA1", "$(tag)"]
build_context = ".."

[container.image.build_args]
VERSION = "${VERSION}"
SHA1 = "${SHA1}"
SNAPSHOT = "${SNAPSHOT}"
EOF