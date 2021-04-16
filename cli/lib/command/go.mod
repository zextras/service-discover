module bitbucket.org/zextras/service-discover/cli/lib/command

go 1.16

replace (
	bitbucket.org/zextras/service-discover/cli/lib/credentialsEncrypter => ./../credentialsEncrypter
	bitbucket.org/zextras/service-discover/cli/lib/exec => ./../exec
	bitbucket.org/zextras/service-discover/cli/lib/formatter => ./../formatter
	bitbucket.org/zextras/service-discover/cli/lib/test => ./../test
	bitbucket.org/zextras/service-discover/cli/lib/zimbra => ./../zimbra
)

require (
	bitbucket.org/zextras/service-discover/cli/lib/exec v0.0.0-00010101000000-000000000000
	bitbucket.org/zextras/service-discover/cli/lib/formatter v0.0.0-00010101000000-000000000000
	bitbucket.org/zextras/service-discover/cli/lib/test v0.0.0-00010101000000-000000000000
	bitbucket.org/zextras/service-discover/cli/lib/zimbra v0.0.0-00010101000000-000000000000
	github.com/pkg/errors v0.9.1
	github.com/stretchr/testify v1.7.0
	golang.org/x/crypto v0.0.0-20210218145215-b8e89b74b9df // indirect
)
