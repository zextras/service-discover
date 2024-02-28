module github.com/Zextras/service-discover/service-discoverd

go 1.16

replace (
	github.com/Zextras/service-discover/cli/lib/carbonio => ./../cli/lib/carbonio
	github.com/Zextras/service-discover/cli/lib/test => ./../cli/lib/test
)

require (
	github.com/Zextras/service-discover/cli/lib/carbonio v0.0.0-00010101000000-000000000000
	github.com/stretchr/testify v1.8.4
)
