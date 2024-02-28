module github.com/Zextras/service-discover/cli/lib/command

go 1.16

replace (
	github.com/Zextras/service-discover/cli/lib/carbonio => ./../carbonio
	github.com/Zextras/service-discover/cli/lib/credentialsEncrypter => ./../credentialsEncrypter
	github.com/Zextras/service-discover/cli/lib/exec => ./../exec
	github.com/Zextras/service-discover/cli/lib/formatter => ./../formatter
	github.com/Zextras/service-discover/cli/lib/term => ./../term
	github.com/Zextras/service-discover/cli/lib/test => ./../test
)

require (
	github.com/Zextras/service-discover/cli/lib/carbonio v0.0.0-00010101000000-000000000000
	github.com/Zextras/service-discover/cli/lib/credentialsEncrypter v0.0.0-00010101000000-000000000000
	github.com/Zextras/service-discover/cli/lib/exec v0.0.0-00010101000000-000000000000
	github.com/Zextras/service-discover/cli/lib/formatter v0.0.0-00010101000000-000000000000
	github.com/Zextras/service-discover/cli/lib/term v0.0.0-00010101000000-000000000000
	github.com/Zextras/service-discover/cli/lib/test v0.0.0-00010101000000-000000000000
	github.com/go-ldap/ldap/v3 v3.2.4
	github.com/pkg/errors v0.9.1
	github.com/stretchr/testify v1.8.4
	github.com/testcontainers/testcontainers-go v0.28.0
)
