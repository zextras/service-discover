module bitbucket.org/zextras/service-discover/service-discoverd

go 1.16

replace (
	bitbucket.org/zextras/service-discover/cli/lib/zimbra => ./../cli/lib/zimbra
	bitbucket.org/zextras/service-discover/cli/lib/test => ./../cli/lib/test
)

require (
	bitbucket.org/zextras/service-discover/cli/lib/zimbra v0.0.0-00010101000000-000000000000
	github.com/stretchr/testify v1.7.0
)