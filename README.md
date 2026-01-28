# Zextras Service Discover

Service Discover is a service discovery and configuration management solution based on Consul for Zextras services. It provides service registration, health monitoring, and distributed configuration capabilities essential for microservices architecture.

## Table of Contents

- [Project Structure](#project-structure)
- [Prerequisites](#prerequisites)
- [Building](#building)
- [Testing](#testing)
- [Contributing](#contributing)
- [License](#license)

## Project Structure

```text
cmd/                # Main applications (agent, server, service-discoverd)
pkg/                # Go packages (encrypter, exec, formatter, parser, etc.)
build/              # Packaging scripts and configs (yap.json, PKGBUILD, etc.)
test/               # Test utilities and mocks
build_packages.sh   # Build helper script (Not for production uses)
Jenkinsfile         # CI/CD pipeline
```

## Prerequisites

- Go 1.20+ (recommended)
- Docker (for integration tests)
- Ubuntu 20.04/22.04 or RHEL 8/9 for packaging
- [yap](https://github.com/zextras/yap) (for building .deb/.rpm packages)
- Make, git, and standard build tools

## Building

### Clone the repository

```sh
git clone <repo-url>
cd service-discover
```

### Building Packages

```bash
# Build packages for Ubuntu 22.04
make build TARGET=ubuntu-jammy

# Build packages for Rocky Linux 9
make build TARGET=rocky-9

# Build packages for Ubuntu 24.04
make build TARGET=ubuntu-noble
```

### Supported Targets

- `ubuntu-jammy` - Ubuntu 22.04 LTS
- `ubuntu-noble` - Ubuntu 24.04 LTS
- `rocky-8` - Rocky Linux 8
- `rocky-9` - Rocky Linux 9

### Configuration

You can customize the build by setting environment variables:

```bash
# Use a specific container runtime
make build TARGET=ubuntu-jammy CONTAINER_RUNTIME=docker

# Use a different output directory
make build TARGET=rocky-9 OUTPUT_DIR=./my-packages
```

### IDE Setup

If using an IDE like IntelliJ IDEA:

1. Enable Go module support in Go settings
2. The IDE should automatically detect modules and download dependencies

## Testing

### Unit and Integration Tests

This project uses [gotestsum](https://github.com/gotestyourself/gotestsum) for running tests and generating reports.

To run all tests:

```sh
go run gotest.tools/gotestsum@latest --format testname --junitfile tests.xml ./...
```

Or run tests for a specific module:

```sh
cd pkg/encrypter
go run gotest.tools/gotestsum@latest --format testname --junitfile tests.xml
```

### Integration Environment

Some tests require `service-discover-base` to be installed on the system. See Jenkinsfile for setup steps.

## Deployment & Release

Artifacts are uploaded to Artifactory and promoted via Jenkins pipeline. See Jenkinsfile for details on upload and promotion steps.

Please ensure all tests pass and code is linted before submitting.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for information on how to contribute to this project.

## License

This project is licensed under the GNU Affero General Public License v3.0 - see the [COPYING](COPYING) file for details.

