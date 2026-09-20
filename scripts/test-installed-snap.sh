#!/usr/bin/bash

set -euo pipefail

if [[ $# -ne 1 ]]; then
    echo "usage: $0 <snap-file>" >&2
    exit 2
fi

repo_root=$(realpath "$(dirname "$0")/..")
snap_path=$(realpath "$1")
expected="$repo_root/testdata/minimal-test.expected"
temp_parent=${RUNNER_TEMP:-$HOME}
temp_dir=$(mktemp -d "$temp_parent/ifacetool-test.XXXXXX")
trap 'rm -rf "$temp_dir"' EXIT

sudo snap install --dangerous "$snap_path"

export PATH="/snap/bin:$PATH"
output="$temp_dir/minimal-test.out"
(
    cd "$temp_dir"
    "$repo_root/minimal-test" | tee "$output"
)

diff -u "$expected" "$output"