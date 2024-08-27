#!/bin/bash
#
# SPDX-FileCopyrightText: 2022-2024 Zextras <https://www.zextras.com>
#
# SPDX-License-Identifier: AGPL-3.0-only
#

docker run --rm --entrypoint=/bin/bash \
  -v "$(pwd)/artifacts:/artifacts" \
  -v "$(pwd):/tmp/service-discover" \
  registry.dev.zextras.com/jenkins/pacur/ubuntu-20.04:v2 \
  -c "yap build ubuntu-focal /tmp/service-discover/build -sd"
