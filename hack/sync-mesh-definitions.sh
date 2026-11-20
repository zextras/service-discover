#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
#
# SPDX-License-Identifier: AGPL-3.0-only

# Refreshes pkg/mesh/definitions from the package repositories listed in
# pkg/mesh/definitions/index.json. Every file entry of a service records the repository and the path it was copied from,
# so the refresh is a plain re-download. Review the resulting diff before committing it.
#
# Usage: hack/sync-mesh-definitions.sh [git-ref]   (default: the default branch of every repository)

set -euo pipefail

ref="${1:-}"
dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/../pkg/mesh/definitions" && pwd)"

jq -r '.services[] | .name as $n | .repo as $r | .files[] | [$n, $r, .name, .path] | @tsv' "${dir}/index.json" |
  while IFS=$'\t' read -r service repo name path; do
    url="repos/${repo}/contents/${path}"
    [[ -n "${ref}" ]] && url="${url}?ref=${ref}"
    echo "${service}/${name} <- ${repo}:${path}"
    gh api -H 'Accept: application/vnd.github.raw' "${url}" >"${dir}/${service}/${name}.tmp"
    mv "${dir}/${service}/${name}.tmp" "${dir}/${service}/${name}"
  done
