module github.com/Zextras/service-discover/service-discoverd

go 1.22

replace (
	github.com/Zextras/service-discover/cli/lib/carbonio => ./../cli/lib/carbonio
	github.com/Zextras/service-discover/cli/lib/test => ./../cli/lib/test
)

require (
	github.com/Zextras/service-discover/cli/lib/carbonio v0.0.0-00010101000000-000000000000
	github.com/stretchr/testify v1.9.0
)

require (
	dario.cat/mergo v1.0.0 // indirect
	github.com/Azure/go-ntlmssp v0.0.0-20200615164410-66371956d46c // indirect
	github.com/cenkalti/backoff/v4 v4.3.0 // indirect
	github.com/containerd/containerd v1.7.16 // indirect
	github.com/cpuguy83/dockercfg v0.3.1 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/distribution/reference v0.6.0 // indirect
	github.com/docker/docker v26.1.1+incompatible // indirect
	github.com/docker/go-connections v0.5.0 // indirect
	github.com/docker/go-units v0.5.0 // indirect
	github.com/go-asn1-ber/asn1-ber v1.5.1 // indirect
	github.com/go-ldap/ldap/v3 v3.2.4 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/magiconair/properties v1.8.7 // indirect
	github.com/moby/docker-image-spec v1.3.1 // indirect
	github.com/moby/patternmatcher v0.6.0 // indirect
	github.com/moby/term v0.5.0 // indirect
	github.com/morikuni/aec v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.1.0 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/shirou/gopsutil/v3 v3.24.4 // indirect
	github.com/stretchr/objx v0.5.2 // indirect
	golang.org/x/crypto v0.14.0 // indirect
	golang.org/x/exp v0.0.0-20240416160154-fe59bbe5cc7f // indirect
	golang.org/x/sys v0.19.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
