module bitbucket.org/zextras/service-discover/cli/server

go 1.16

replace (
	bitbucket.org/zextras/service-discover/cli/lib/command => ./../lib/command
	bitbucket.org/zextras/service-discover/cli/lib/credentialsEncrypter => ./../lib/credentialsEncrypter
	bitbucket.org/zextras/service-discover/cli/lib/exec => ./../lib/exec
	bitbucket.org/zextras/service-discover/cli/lib/formatter => ./../lib/formatter
	bitbucket.org/zextras/service-discover/cli/lib/permissions => ./../lib/permissions
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
	bitbucket.org/zextras/service-discover/cli/lib/permissions v0.0.0-00010101000000-000000000000
	bitbucket.org/zextras/service-discover/cli/lib/parser v0.0.0-00010101000000-000000000000
	bitbucket.org/zextras/service-discover/cli/lib/systemd v0.0.0-00010101000000-000000000000
	bitbucket.org/zextras/service-discover/cli/lib/term v0.0.0-00010101000000-000000000000
	bitbucket.org/zextras/service-discover/cli/lib/test v0.0.0-00010101000000-000000000000
	bitbucket.org/zextras/service-discover/cli/lib/zimbra v0.0.0-00010101000000-000000000000
	github.com/alecthomas/kong v0.2.15
	github.com/coreos/go-systemd/v22 v22.2.0
	github.com/kr/pretty v0.1.0 // indirect
	github.com/pkg/errors v0.9.1
	github.com/stretchr/objx v0.1.1 // indirect
	github.com/stretchr/testify v1.7.0
	gopkg.in/check.v1 v1.0.0-20180628173108-788fd7840127 // indirect
)
