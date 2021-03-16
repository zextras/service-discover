Service-discover CLI
===

This folder contains the CLI of the service-discover service. This is a separate
Go module project.

# OCI images for manual testing

A docker image allows you to play with the server CLI without messing up your system. Note: Centos8 image gets stuck on
the login screen for some reason.

## Building and running the images

Open a shell and type:
```shell
podman build -t test/test:ubu -f Dockerfile.ubuntu18.server . && podman run --rm -it test/test:ubu
```

For Ubuntu 20.04, while for Centos8 you have to type:
```shell
podman build -t test/test:cent -f Dockerfile.centos8.server . && podman run --rm -it test/test:cent
```

Please note that those images only works with **Podman**, and not Docker!