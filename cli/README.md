Service-discover CLI
===

This folder contains the CLI of the service-discover service. This is a separate
Go module project.

# OCI images for manual testing

A docker image allows you to play with the server/agent CLI without messing up your system. Note: Centos8 image gets
stuck on the login screen for some reason 🤷.

## Building and running the images

Open a shell and type:
```shell
podman build -t test/test:ubu -f Dockerfile.ubuntu18.server . && podman run --rm -it test/test:ubu
```

Please note that those images only works with **Podman**, and not Docker! This because Docker doesn't support systemd.

## How to try service-discover cluster (server + agent)

You can try the interoperation between server and agent with the `docker-compose.yml` file you find in the very same
folder. Since the images work only with Podman, you'll have to install 
[podman-compose](https://github.com/containers/podman-compose), and run them with `podman-compose up`. If you want to
attach to the running instances, you only have to open another terminal and run `podman attach <container_id>`. This 
will give you an interactive shell.