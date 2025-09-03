#!/bin/bash
#
# SPDX-FileCopyrightText: 2022-2024 Zextras <https://www.zextras.com>
#
# SPDX-License-Identifier: AGPL-3.0-only
#

OS=${1:-"ubuntu-jammy"}

echo "Building for OS: $OS"

docker run -it --rm \
    --entrypoint=/bin/bash \
    -v "$(pwd)/artifacts/${OS}":/artifacts \
    -v "$(pwd)":/tmp/project \
    "docker.io/m0rf30/yap-${OS}:1.8" \
    -c "yap prepare ${OS} -g && yap build ${OS} /tmp/project/build"
