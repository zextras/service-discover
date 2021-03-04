module bitbucket.org/zextras/service-discover/cli/agent

go 1.16

replace bitbucket.org/zextras/service-discover/cli/lib/command => ./../lib/command

replace bitbucket.org/zextras/service-discover/cli/lib/formatter => ./../lib/formatter

replace bitbucket.org/zextras/service-discover/cli/lib/parser => ./../lib/parser

require (
	bitbucket.org/zextras/service-discover/cli/lib/command v0.0.0-00010101000000-000000000000
	bitbucket.org/zextras/service-discover/cli/lib/parser v0.0.0-00010101000000-000000000000
	github.com/alecthomas/kong v0.2.12
	github.com/pkg/errors v0.9.1
	github.com/stretchr/testify v1.7.0
)
