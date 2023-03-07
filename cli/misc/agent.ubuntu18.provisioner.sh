#!/usr/bin/env bash

#
# Copyright (C) 2023 Zextras srl
#
#     This program is free software: you can redistribute it and/or modify
#     it under the terms of the GNU Affero General Public License as published by
#     the Free Software Foundation, either version 3 of the License, or
#     (at your option) any later version.
#
#     This program is distributed in the hope that it will be useful,
#     but WITHOUT ANY WARRANTY; without even the implied warranty of
#     MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
#     GNU Affero General Public License for more details.
#
#     You should have received a copy of the GNU Affero General Public License
#     along with this program.  If not, see <https://www.gnu.org/licenses/>.
#
#

set -e

apt-get update
apt-get install -yqq \
  curl \
  ca-certificates \
  iproute2 \
  lsb-release \
  gnupg2 \
  ifupdown2 \
  net-tools \
  jq \
  less
echo "deb [trusted=yes] https://repo.zextras.io/rc/ubuntu focal main" >/etc/apt/sources.list.d/zextras.list
apt-get update
apt-get install -y service-discover-base service-discover-agent systemd-sysv dbus sudo
echo 'LANG="en_US.UTF-8"' >/etc/default/locale
rm -rf /var/lib/apt/lists/*
