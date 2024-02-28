module github.com/Zextras/service-discover/cli/lib/credentialsEncrypter

replace github.com/Zextras/service-discover/cli/lib/test => ./../test

go 1.15

require (
	github.com/Zextras/service-discover/cli/lib/test v0.0.0-00010101000000-000000000000
	github.com/pkg/errors v0.9.1
	github.com/stretchr/testify v1.8.4
	golang.org/x/crypto v0.14.0
)
