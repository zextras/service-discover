/*
 * Copyright (C) 2023 Zextras srl
 *
 *     This program is free software: you can redistribute it and/or modify
 *     it under the terms of the GNU Affero General Public License as published by
 *     the Free Software Foundation, either version 3 of the License, or
 *     (at your option) any later version.
 *
 *     This program is distributed in the hope that it will be useful,
 *     but WITHOUT ANY WARRANTY; without even the implied warranty of
 *     MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 *     GNU Affero General Public License for more details.
 *
 *     You should have received a copy of the GNU Affero General Public License
 *     along with this program.  If not, see <https://www.gnu.org/licenses/>.
 *
 */

package config

// Please note that these variables could be automatically populated in the future
const (
	ApplicationName        = "service-discover"
	ApplicationDescription = "CLI utility to interact with a service-discover server node"
	ApplicationVersion     = "0.1.0"
	AgentName              = "agent"
	ConsulHome             = "/var/lib/service-discover"
	ConsulData             = ConsulHome + "/data"
	ConsulConfig           = "/etc/zextras/service-discover"
	ConsultFileConfig      = ConsulConfig + "/main.json"
	LocalConfigPath        = "/opt/zextras/conf/localconfig.xml"
	ClusterCredential      = ConsulConfig + "/cluster-credentials.tar.gpg"
	LDAPDNName             = "service-discover-credentials"
)
