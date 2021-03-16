#!/usr/bin/env bash

set -e

FILENAME="ALREADY_INSTALLED"

echo "Checking if Zimbra is already installed..."

if [[ -e ${FILENAME} ]]
then
  echo "Skipping installation"
  exit 0
fi


(cd /zcs-* && /bin/imahuman.sh | ./install.sh)

touch /${FILENAME}

echo "Installation completed ;)"
exit 0