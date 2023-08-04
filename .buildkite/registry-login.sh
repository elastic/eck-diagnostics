#!/bin/bash

set -eu

username=$(vault read -field=username "$DOCKER_REGISTRY_VAULT_PATH")
password=$(vault read -field=password "$DOCKER_REGISTRY_VAULT_PATH")

buildah login --username="${username}" --password="${password}" "$DOCKER_REGISTRY_HOSTNAME"
