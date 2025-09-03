#!/bin/bash

go build -o build/server/server cmd/server/server.go
go build -o build/agent/agent cmd/agent/agent.go
go build -o build/service-discoverd/service-discoverd cmd/service-discoverd/service-discoverd.go