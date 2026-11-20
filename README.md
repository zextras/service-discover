# Zextras Service Discover

Service Discover is a service discovery and configuration management solution based on Consul for Zextras services. It provides service registration, health monitoring, and distributed configuration capabilities essential for microservices architecture.

## Table of Contents

- [Project Structure](#project-structure)
- [Service definitions](#service-definitions)
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

## Service definitions

What the mesh needs to know about each Carbonio service (its Consul policy, role and intentions, its SPIRE identity,
the user it runs as, the units to restart, the commands of its setup) is **not** in this repository: it is in the
[`service-discover-specs`](https://github.com/zextras/service-discover-specs) repository, shipped as the
`service-discover-specs` package, which `service-discover-server` and `service-discover-agent` depend on. A new
service is added there, and needs no release of service-discover.

- The definitions are read from `/usr/share/service-discover/specs` (`SERVICE_DISCOVER_SPECS_DIR` points the commands
  to another directory, for tests and development). They are never read from Consul or from a directory a node can
  write: the installed package has to be owned by root, not writable by others, and match the sha256 of its
  `manifest.json`, otherwise the commands refuse it.
- On the servers, the `service-discover-mesh-apply.path` unit watches `manifest.json`: when the package changes, the
  unit runs `service-discover mesh apply --unattended`. Only the Raft leader applies the definitions to Consul; a
  server running the SPIRE server registers the SPIRE entries of all the nodes. The bootstrap token comes from the
  password the setup stored in `/var/lib/service-discover/password`; a leader without it logs that it did nothing.
- On the nodes, `service-discover-sync.path` runs `service sync` when `manifest.json` changes. A service whose
  definition the servers have not applied yet (the package reached the node first) makes it end with the status 75,
  and the unit runs it again after a minute. `service pending-setup` (the setup at the installation of a package) ends
  with the same status 75 and a message, keeping the setup pending: it is not an error.
- `service-discover mesh validate <dir>` checks a directory of definitions without Consul: the checks of the loader
  and the rules across the files of a definition. The CI of `service-discover-specs` runs it.
- `mesh apply --unattended` refuses `--revoke-legacy` on purpose: the revocation deletes the legacy token of each
  service, the way back if a node did not really switch to its SPIRE login, and a package upgrade must not do it
  without an operator having looked at the result of `service sync`. Run `mesh apply --revoke-legacy` by hand.
- The tests use the subset of the definitions in `pkg/mesh/testdata/specs`.

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

