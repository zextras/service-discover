# Zextras service-discover

This repository contains all the necessary pieces (CLI, daemon and packages
configuration) to build and distribute the service-discover based on Consul for
Zextras services.

## Table of Contents
- [Project Structure](#project-structure)
- [Prerequisites](#prerequisites)
- [Build Instructions](#build-instructions)
- [Testing](#testing)
- [Deployment & Release](#deployment--release)
- [License](#license)

## Project Structure
```
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

## Build Instructions

### IDE Setup
If using an IDE like Intellij Idea, Ensure that you have go module support enabled in Go settings section.
Then the IDE should automatically detect the modules and download dependencies.

### Install Go dependencies
```sh
go mod download
```

### Build Binaries
To build the main binaries:
```sh
go build -o bin/agent ./cmd/agent
go build -o bin/server ./cmd/server
go build -o bin/service-discoverd ./cmd/service-discoverd
```

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

## Deployment & Release

Artifacts are uploaded to Artifactory and promoted via Jenkins pipeline. See Jenkinsfile for details on upload and promotion steps.

Please ensure all tests pass and code is linted before submitting.

## RC
Release is managed with [release-it](https://github.
com/release-it/release-it).
Install the dependencies with `npm i`.  
Run `release-it --ci`. This will bump the versions, commit, tag and push the 
code.  
The make sure the tag was built. This will deliver the RC.  
Finalize the work by merging the RC in the main branch. 


## License

This project is licensed under the terms of the COPYING file.

