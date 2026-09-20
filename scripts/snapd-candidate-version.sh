#!/usr/bin/bash

set -euo pipefail

mapfile -t versions < <(
    snap info snapd | awk '$1 == "latest/candidate:" { print $2 }'
)

if [[ ${#versions[@]} -ne 1 ]]; then
    echo "expected one latest/candidate version for snapd, found ${#versions[@]}" >&2
    exit 1
fi

version=${versions[0]}
if [[ ! $version =~ ^[0-9]+(\.[0-9]+)*$ ]]; then
    echo "invalid snapd candidate version: $version" >&2
    exit 1
fi

printf '%s\n' "$version"