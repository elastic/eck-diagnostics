#!/bin/sh
#
# Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
# or more contributor license agreements. Licensed under the Elastic License 2.0;
# you may not use this file except in compliance with the Elastic License 2.0.
#

# Script to copy the eck-diagnostics binary in the current directory from the container image.
# It is intended to be displayed on stdout and piped into sh.

set -eu

os=$(uname | tr '[:upper:]' '[:lower:]')
arch=$(uname -m | sed -e "s|x86_|amd|" -e "s|aarch|arm|")

docker run --rm -v $(pwd):/tmp --entrypoint cp "{{IMAGE_NAME}}:{{IMAGE_TAG}}" \
    -f "/artefacts/eck-diagnostics-${os}-${arch}" /tmp/eck-diagnostics

echo "eck-diagnostics-${os}-${arch} binary copied in local ✅"
echo
echo "./eck-diagnostics -v"
./eck-diagnostics -v
