#!/bin/bash
#
# SPDX-FileCopyrightText: 2023 Zextras <https://www.zextras.com>
#
# SPDX-License-Identifier: GPL-2.0-only
#

docker run --rm --entrypoint=/bin/bash \
  -v "$(pwd)/artifacts:/artifacts" \
  -v "$(pwd):/tmp/staging" \
  registry.dev.zextras.com/jenkins/pacur/ubuntu-20.04:v2 \
  -c "cp -r /tmp/staging/** /tmp && cd /tmp/staging && yap build ubuntu-focal ."
