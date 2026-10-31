# Running Opscope on your machine

This guide is for trying Opscope on your own computer, against clusters you already use. It takes a
couple of minutes and needs only Docker. To run Opscope for a team, see
[Deploying Opscope](deployment.md). To work on Opscope's code, see
[CONTRIBUTING.md](../CONTRIBUTING.md).

- [What you need](#what-you-need)
- [Run it with Docker](#run-it-with-docker)
- [Start with a cluster from a kubeconfig file](#start-with-a-cluster-from-a-kubeconfig-file)
- [Reaching the cluster from the container](#reaching-the-cluster-from-the-container)
- [Stop and clean up](#stop-and-clean-up)

## What you need

- [Docker](https://docs.docker.com/get-docker/), or another tool that runs container images.
- A Kubernetes cluster you can reach, and a kubeconfig for it. What Opscope shows is limited to what
  that kubeconfig's user may read.

Opscope never reads `~/.kube/config` on its own; you give it a cluster in one of the two ways below.

## Run it with Docker

```bash
docker run --rm -p 127.0.0.1:8080:8080 -v opscope-data:/data ghcr.io/unnikrishnannam/opscope:latest
```

Open http://localhost:8080, choose *Add a cluster*, and paste or upload a kubeconfig, then pick a
context. Opscope checks that it can connect, then saves only that context, in the `opscope-data`
volume, so it's still there next time.

A kubeconfig added in the UI must have its credentials embedded: no login commands or file paths.
This prints your current context in that form:

```bash
kubectl config view --minify --flatten
```

The port is published on `127.0.0.1` only, because Opscope has no login of its own: anyone who can
open it can read what it reads.

## Start with a cluster from a kubeconfig file

Instead of adding a cluster in the UI, mount a kubeconfig file read-only and point
`OPSCOPE_KUBECONFIG` at it:

```bash
docker run --rm -p 127.0.0.1:8080:8080 -v opscope-data:/data \
  -v "$HOME/.kube/lab.kubeconfig:/config/kubeconfig:ro" \
  -e OPSCOPE_KUBECONFIG=/config/kubeconfig \
  ghcr.io/unnikrishnannam/opscope:latest
```

That cluster is loaded at startup and marked "Environment" in the UI. Set `OPSCOPE_CONTEXT` to use
a context other than the file's current one. Unlike kubeconfigs added in the UI, this file may use
login commands (`exec`, as cloud CLI plugins do), provided the command exists in the image; the image
is minimal and contains no cloud CLIs, so embedded credentials are the reliable choice in a
container.

The container runs as a non-root user (uid 65532). On Linux, the mounted file must be readable by
that user.

Every setting is listed under [Configuration](deployment.md#configuration).

## Reaching the cluster from the container

The container must be able to reach the cluster's API server. A `server:` address of `127.0.0.1`
or `localhost` in the kubeconfig points at the container itself, not at your machine.

**kind clusters** publish their API server on your machine's `127.0.0.1`. Put Opscope on kind's
Docker network instead, and use the kubeconfig kind writes for that network:

```bash
kind get kubeconfig --internal --name <cluster> > kind.internal.kubeconfig
```

Then add `--network kind` to `docker run`, or connect a running container:

```bash
docker network connect kind <opscope-container>
```

## Stop and clean up

Press Ctrl+C in the terminal running Opscope; `--rm` removes the container. The clusters you added
stay in the `opscope-data` volume for next time. To remove them too:

```bash
docker volume rm opscope-data
```

When you're ready to give Opscope to your team, continue with [Deploying Opscope](deployment.md).
