module bitbucket.org/zextras/service-discover/cli/agent

go 1.16

replace (
	bitbucket.org/zextras/service-discover/cli/lib/command => ./../lib/command
	bitbucket.org/zextras/service-discover/cli/lib/credentialsEncrypter => ./../lib/credentialsEncrypter
	bitbucket.org/zextras/service-discover/cli/lib/exec => ./../lib/exec
	bitbucket.org/zextras/service-discover/cli/lib/formatter => ./../lib/formatter
	bitbucket.org/zextras/service-discover/cli/lib/parser => ./../lib/parser
	bitbucket.org/zextras/service-discover/cli/lib/systemd => ./../lib/systemd
	bitbucket.org/zextras/service-discover/cli/lib/term => ./../lib/term
	bitbucket.org/zextras/service-discover/cli/lib/test => ./../lib/test
	bitbucket.org/zextras/service-discover/cli/lib/zimbra => ./../lib/zimbra
)

require (
	bitbucket.org/zextras/service-discover/cli/lib/command v0.0.0-00010101000000-000000000000
	bitbucket.org/zextras/service-discover/cli/lib/credentialsEncrypter v0.0.0-00010101000000-000000000000
	bitbucket.org/zextras/service-discover/cli/lib/exec v0.0.0-00010101000000-000000000000
	bitbucket.org/zextras/service-discover/cli/lib/formatter v0.0.0-00010101000000-000000000000
	bitbucket.org/zextras/service-discover/cli/lib/parser v0.0.0-00010101000000-000000000000
	bitbucket.org/zextras/service-discover/cli/lib/systemd v0.0.0-00010101000000-000000000000
	bitbucket.org/zextras/service-discover/cli/lib/term v0.0.0-00010101000000-000000000000
	bitbucket.org/zextras/service-discover/cli/lib/test v0.0.0-00010101000000-000000000000
	bitbucket.org/zextras/service-discover/cli/lib/zimbra v0.0.0-00010101000000-000000000000
	github.com/alecthomas/kong v0.2.12
	github.com/coreos/go-systemd/v22 v22.2.0
	github.com/hashicorp/consul/api v1.8.1
	github.com/pkg/errors v0.9.1
	github.com/stretchr/testify v1.7.0
)
