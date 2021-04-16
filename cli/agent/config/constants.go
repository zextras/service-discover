package config

// Please note that these variables could be automatically populated in the future
const (
	ApplicationName        = "service-discover"
	ApplicationDescription = "CLI utility to interact with a service-discover agent node"
	ApplicationVersion     = "0.1.0"
	AgentName              = "agent"
	ConsulHome             = "/var/lib/service-discover"
	ConsulData             = ConsulHome + "/data"
	ConsulConfig           = "/etc/zextras/service-discover"
	ConsultFileConfig      = ConsulConfig + "/main.json"
	LocalConfigPath        = "/opt/zimbra/conf/localconfig.xml"
	ClusterCredential      = ConsulConfig + "/cluster-credentials.tar.gpg"
)
