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

FILENAME="ZIMBRA_ALREADY_INSTALLED"

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
