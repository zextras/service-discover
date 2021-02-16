module bitbucket.org/zextras/service-discover/cli/server

go 1.15

replace bitbucket.org/zextras/service-discover/cli/lib/command => ./../lib/command

replace bitbucket.org/zextras/service-discover/cli/lib/formatter => ./../lib/formatter

replace bitbucket.org/zextras/service-discover/cli/lib/parser => ./../lib/parser

require (
	bitbucket.org/zextras/service-discover/cli/lib/command v0.0.0-20210205112328-4bc21a429e9d
	bitbucket.org/zextras/service-discover/cli/lib/parser v0.0.0-20210127130406-2da1389a2a40
	github.com/alecthomas/kong v0.2.12
)
