#!/usr/bin/env bash

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
