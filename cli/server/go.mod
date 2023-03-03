module github.com/Zextras/service-discover/cli/server

go 1.16

replace (
	github.com/Zextras/service-discover/cli/lib/carbonio => ./../lib/carbonio
	github.com/Zextras/service-discover/cli/lib/command => ./../lib/command
	github.com/Zextras/service-discover/cli/lib/credentialsEncrypter => ./../lib/credentialsEncrypter
	github.com/Zextras/service-discover/cli/lib/exec => ./../lib/exec
	github.com/Zextras/service-discover/cli/lib/formatter => ./../lib/formatter
	github.com/Zextras/service-discover/cli/lib/parser => ./../lib/parser
	github.com/Zextras/service-discover/cli/lib/permissions => ./../lib/permissions
	github.com/Zextras/service-discover/cli/lib/systemd => ./../lib/systemd
	github.com/Zextras/service-discover/cli/lib/term => ./../lib/term
	github.com/Zextras/service-discover/cli/lib/test => ./../lib/test
)

require (
	github.com/Zextras/service-discover/cli/lib/carbonio v0.0.0-00010101000000-000000000000
	github.com/Zextras/service-discover/cli/lib/command v0.0.0-00010101000000-000000000000
	github.com/Zextras/service-discover/cli/lib/credentialsEncrypter v0.0.0-00010101000000-000000000000
	github.com/Zextras/service-discover/cli/lib/exec v0.0.0-00010101000000-000000000000
	github.com/Zextras/service-discover/cli/lib/formatter v0.0.0-00010101000000-000000000000
	github.com/Zextras/service-discover/cli/lib/parser v0.0.0-00010101000000-000000000000
	github.com/Zextras/service-discover/cli/lib/permissions v0.0.0-00010101000000-000000000000
	github.com/Zextras/service-discover/cli/lib/systemd v0.0.0-00010101000000-000000000000
	github.com/Zextras/service-discover/cli/lib/term v0.0.0-00010101000000-000000000000
	github.com/Zextras/service-discover/cli/lib/test v0.0.0-00010101000000-000000000000
	github.com/alecthomas/kong v0.2.15
	github.com/coreos/go-systemd/v22 v22.3.2
	github.com/go-ldap/ldap/v3 v3.2.4
	github.com/pkg/errors v0.9.1
	github.com/stretchr/testify v1.8.0
	github.com/testcontainers/testcontainers-go v0.14.0
)
