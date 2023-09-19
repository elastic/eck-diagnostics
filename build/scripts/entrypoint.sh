#!/bin/sh

# Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
# or more contributor license agreements. Licensed under the Elastic License 2.0;
# you may not use this file except in compliance with the Elastic License 2.0.

set -eu

help() {
	echo '
Copy the eck-diagnostics binary in the current directory from the container image using:

  docker run --rm '"$IMAGE_NAME"':'"$IMAGE_TAG"' get | sh'
}

case "${1:-}" in
	get)
		sed -e "s|{{IMAGE_NAME}}|$IMAGE_NAME|" -e "s|{{IMAGE_TAG}}|$IMAGE_TAG|" /usr/local/bin/get.sh
	;;
	*)
		help
	;;
esac
