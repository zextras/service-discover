#!/bin/bash
#
# SPDX-FileCopyrightText: 2022-2024 Zextras <https://www.zextras.com>
#
# SPDX-License-Identifier: AGPL-3.0-only
#

docker run -it --rm \
    --entrypoint=yap \
    -v "$(pwd)"/artifacts/ubuntu-jammy:/artifacts \
    -v "$(pwd)":/tmp/project \
    --entrypoint /bin/bash \
    docker.io/m0rf30/yap-ubuntu-jammy:1.8 \
    -c "yap prepare ubuntu-jammy -g && yap build ubuntu-jammy /tmp/project/build -sd"