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
apt-get install -y \
  curl \
  ca-certificates \
  iproute2 \
  lsb-release \
  gnupg2 \
  ifupdown2 \
  net-tools \
  jq \
  less
(cd / && curl -ssL https://files.zimbra.com/downloads/8.8.15_GA/zcs-8.8.15_GA_3869.UBUNTU18_64.20190918004220.tgz --output - | tar zxvf -)
echo "deb [trusted=yes] https://repo.zextras.io/rc/ubuntu focal main" >/etc/apt/sources.list.d/zextras.list
apt-get update
apt-get install -y service-discover-base service-discover-server systemd-sysv dbus sudo
systemctl disable service-discover.service
echo 'LANG="en_US.UTF-8"' >/etc/default/locale
mv /tmp/zinstall.service /etc/systemd/system/
mv /tmp/imahuman.sh /tmp/zinstaller.sh /bin/
systemctl daemon-reload
systemctl enable zinstall.service
zinstaller.sh
rm -rf /zcs-*
rm -rf /var/lib/apt/lists/*
