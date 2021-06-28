module bitbucket.org/zextras/service-discover/cli/lib/zimbra

replace bitbucket.org/zextras/service-discover/cli/lib/test => ./../test

go 1.16

require (
	bitbucket.org/zextras/service-discover/cli/lib/test v0.0.0-00010101000000-000000000000
	github.com/go-ldap/ldap/v3 v3.2.4
	github.com/stretchr/testify v1.7.0
)
