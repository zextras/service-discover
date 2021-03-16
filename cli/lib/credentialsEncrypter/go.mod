module bitbucket.org/zextras/service-discover/cli/lib/credentialsEncrypter

replace (
	bitbucket.org/zextras/service-discover/cli/lib/command => ./../command
	bitbucket.org/zextras/service-discover/cli/lib/formatter => ./../formatter
	bitbucket.org/zextras/service-discover/cli/lib/parser => ./../parser
	bitbucket.org/zextras/service-discover/cli/lib/test => ./../test
	bitbucket.org/zextras/service-discover/cli/lib/zimbra => ./../zimbra
)

go 1.15

require (
	bitbucket.org/zextras/service-discover/cli/lib/test v0.0.0-00010101000000-000000000000
	github.com/stretchr/testify v1.7.0
	golang.org/x/crypto v0.0.0-20210218145215-b8e89b74b9df
)
