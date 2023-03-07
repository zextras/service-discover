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

echo Y #(agree license)
echo Y #(use carbonio repo)
echo Y #(ldap)
echo Y #(logger)
echo Y #(mta)
echo N #(dns-cache)
echo Y #(smtp)
echo Y #(carbonio store)
echo N #(apache)
echo N #(aspell)
echo Y #(memcached)
echo Y #(proxy)
echo N #(drive)
echo N #(imap beta)
echo N #(chat)
echo Y #Continue?
#echo No #change hostname?
echo No #change domain name?
echo 6 #config store
echo 4 #admin password
echo assext #password
echo r #return
echo a #apply
echo Yes #save
echo # save config
echo Yes #system will be modified
echo "No" #carbonio notify
