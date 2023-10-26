#!/bin/bash

# Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
# or more contributor license agreements. Licensed under the Elastic License 2.0;
# you may not use this file except in compliance with the Elastic License 2.0.

set -eu

get() { vault read -field="$1" "$DOCKER_REGISTRY_VAULT_PATH"; }

buildah login --username="$(get username)" --password="$(get password)" "$(get hostname)"
