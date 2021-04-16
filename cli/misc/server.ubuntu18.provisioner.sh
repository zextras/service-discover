#!/usr/bin/env bash

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
