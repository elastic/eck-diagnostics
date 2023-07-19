#!/usr/bin/env bash

# Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
# or more contributor license agreements. Licensed under the Elastic License 2.0;
# you may not use this file except in compliance with the Elastic License 2.0.

set -eu

tag() {
    tag="$VERSION"
    if [[ "$SNAPSHOT" == "true" ]]; then
        tag="$tag-SNAPSHOT"
    fi
    echo "$tag"
}

cat <<EOF
[container.image]
names = ["docker.elastic.co/eck-dev/eck-diagnostics"]
tags = ["latest", "$SHA1", "$(tag)"]
build_context = ".."

[container.image.build_args]
VERSION = "${VERSION}"
SHA1 = "${SHA1}"
SNAPSHOT = "${SNAPSHOT}"
EOF