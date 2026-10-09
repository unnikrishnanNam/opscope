# Opscope

A lightweight, read-only Kubernetes dashboard for day-to-day operations.

[![CI](https://github.com/unnikrishnanNam/opscope/actions/workflows/ci.yml/badge.svg)](https://github.com/unnikrishnanNam/opscope/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/unnikrishnanNam/opscope)](https://github.com/unnikrishnanNam/opscope/releases/latest)
[![Image](https://img.shields.io/badge/image-ghcr.io-blue)](https://github.com/unnikrishnanNam/opscope/pkgs/container/opscope)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue)](LICENSE)

https://github.com/user-attachments/assets/68c13ac5-ac03-4cff-af9e-cf6c682194e9

*A 48-second tour of Opscope and its command palette.*

Opscope answers the everyday questions about a cluster in a browser: is anything unhealthy, why is
this pod restarting, what does this Service route to, what did that job log. It shows nodes,
workloads, configuration, networking, events, logs and live resource usage, across as many clusters
as you connect, and finds any of it from one search box.

It is read-only by design. Opscope uses only `get` and `list`, and nothing in it can change a
cluster, so it can be given to anyone who needs to look, such as developers, on-call engineers and
support, without granting write access. It ships as a single container image and runs on your own
machine or inside the cluster it shows.

## Features

- **Cluster overview:** a health summary, nodes and pods by status, CPU and memory usage, every
  resource kind with what needs attention, and recent warning events.
- **Resources:** Nodes; Pods, Deployments, StatefulSets, DaemonSets, Jobs and CronJobs;
  ConfigMaps and Secrets; Services, Ingresses, and Gateway API Gateways, HTTPRoutes and
  GatewayClasses. Tables filter, sort and refresh on their own, with kubectl-style statuses such as
  `CrashLoopBackOff` and `Init:1/2`.
- **Detail pages:** a summary, containers, conditions, events and YAML for every object, links to
  owners and nodes, and pod logs with the previous run and live follow.
- **Live usage:** CPU and memory per node and pod from metrics-server, with the last 15 minutes as
  a trend.
- **Command palette:** press ⌘K (Ctrl+K) to jump to any page, object, namespace or cluster, and to
  run what the current page offers, such as copying a read-only kubectl command.
- **Keyboard shortcuts** for pages and tabs, listed in the app with `?`.
- **Multiple clusters:** connect clusters from kubeconfig files, the pod's service account, or the
  UI, and switch between them.
- **Light and dark themes**, small screens, and keyboard and screen reader support (WCAG 2.1 AA).

## Quick start

**With Docker,** on your own machine:

```bash
docker run --rm -p 127.0.0.1:8080:8080 -v opscope-data:/data ghcr.io/unnikrishnannam/opscope:latest
```

Open http://localhost:8080 and add a cluster by pasting or uploading a kubeconfig.

**In a Kubernetes cluster,** to show that cluster:

```bash
kubectl apply -f https://raw.githubusercontent.com/unnikrishnanNam/opscope/v1.2.0/deploy/kubernetes/opscope.yaml
```

```bash
kubectl -n opscope port-forward svc/opscope 8080:80
```

Open http://localhost:8080. Secrets pages need an extra, optional role; see
[Deploying Opscope](docs/deployment.md) for Secret access, kind clusters, persistent storage and
every setting.

## Connecting clusters

Opscope has no built-in cluster and never reads `~/.kube/config` on its own. There are three ways to
give it one:

1. **A kubeconfig file:** set `OPSCOPE_KUBECONFIG` to its path (and `OPSCOPE_CONTEXT` to pick a
   context). The cluster is loaded at startup and marked "Environment".
2. **Inside the cluster:** set `OPSCOPE_IN_CLUSTER=true` when Opscope runs as a pod. It uses the
   pod's service account, as the manifests in `deploy/kubernetes/` do.
3. **In the UI:** open *Clusters → Add a cluster*, paste or upload a kubeconfig and pick a context.
   Opscope checks that it can connect, then saves only that context. Kubeconfigs added this way
   must have their credentials embedded; `kubectl config view --minify --flatten` prints the current
   context in that form.

Opscope can show only what the kubeconfig's user or the service account is allowed to read. Every
setting is listed under [Configuration](docs/deployment.md#configuration).

## Command palette and shortcuts

Press ⌘K (Ctrl+K on Windows and Linux), or the search box in the top bar, and type:

- a page: "pods", or kubectl's short names such as `po`, `svc` and `cm`
- an object's name, of any kind ("argocd-server"); start with a kind to search only that kind
  (`po api`, `gtw main`). Objects in the selected namespace come first
- a namespace, another cluster ("switch to"), or the theme
- what the current page offers: open a tab, copy the name, the YAML or a kubectl command (`get`,
  `describe`, `logs`; read-only commands only), follow logs, go to the owner or the node

Enter runs the highlighted command, ⌘/Ctrl+Enter opens a link in a new tab, and Escape clears the
search, then closes the palette. Recently used commands come first when nothing is typed.

| Keys                         | Action                                                |
| ---------------------------- | ----------------------------------------------------- |
| `g` then `o` `n` `p` `d` `s` | Overview, Nodes, Pods, Deployments, Services          |
| `g` then `c`                 | Manage clusters                                       |
| `1` `2` `3` `4`              | An object's Summary, YAML, Events and Logs tabs       |
| `/`                          | Filter the current list                               |
| `?`                          | Every shortcut available on the current page          |

Shortcuts are ignored while you type in a field.

## Security

- **Read-only access.** Opscope uses only `get` and `list`, and the Kubernetes manifests grant
  nothing more. See [Permissions](docs/deployment.md#permissions).
- **No login.** Anyone who can open Opscope can read everything it can read. Keep it on
  `127.0.0.1`, reach it through `kubectl port-forward` in a cluster, or put an authenticating proxy
  in front of it. See [Exposing Opscope](docs/deployment.md#exposing-opscope).
- **Secrets** are listed by key name only. A value is fetched only when you choose "Reveal" on one
  key, is never cached, and never appears in the YAML view. Reading Secrets in a cluster needs a
  separate, optional role.
- **Logs** are never cached, since they can contain sensitive data.
- **Kubeconfigs added in the UI** are checked before they're accepted (no login commands or file
  paths, which could run programs or read files on the server), and saved with only the chosen
  context, readable only by Opscope.

## Screenshots

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/screenshots/overview-dark.png">
  <img src="docs/screenshots/overview-light.png" alt="The Opscope overview: a health summary, nodes, pods, CPU and memory usage, every resource kind with what needs attention, and per-node usage">
</picture>

| Pods | A pod's detail page |
| --- | --- |
| <picture><source media="(prefers-color-scheme: dark)" srcset="docs/screenshots/pods-dark.png"><img src="docs/screenshots/pods-light.png" alt="The pods list, with status, readiness, restarts and live usage"></picture> | <picture><source media="(prefers-color-scheme: dark)" srcset="docs/screenshots/pod-dark.png"><img src="docs/screenshots/pod-light.png" alt="A crash-looping pod's detail page: facts, containers and conditions"></picture> |

## Documentation

| Document                                     | Contents                                                              |
| -------------------------------------------- | --------------------------------------------------------------------- |
| [Deploying Opscope](docs/deployment.md)      | Docker, Kubernetes, permissions, Secret access, exposure, configuration, upgrading |
| [HTTP API](docs/api.md)                      | The JSON API behind the UI                                            |
| [Contributing](CONTRIBUTING.md)              | Development setup, project layout, tests, code style, releases       |
| [Design records](docs/design/)               | How and why Opscope was built, feature by feature                    |
| [Releases](https://github.com/unnikrishnanNam/opscope/releases) | What changed in each version                       |

## Roadmap

Planned and proposed work is tracked as
[enhancement issues](https://github.com/unnikrishnanNam/opscope/issues?q=is%3Aissue+is%3Aopen+label%3Aenhancement).
Suggestions are welcome; open a feature request.

## Contributing

Bug reports, feature requests and pull requests are welcome. [CONTRIBUTING.md](CONTRIBUTING.md)
explains the principles every change keeps, how to run Opscope from source, and how changes are
checked.

## License

Opscope is licensed under the [Apache License 2.0](LICENSE).

It bundles these third-party assets, under their own licences:

| Asset                                                  | Licence                                                    |
| ------------------------------------------------------ | ---------------------------------------------------------- |
| [Lucide](https://lucide.dev) icons (a subset)          | ISC, © Lucide Icons and Contributors                       |
| [Manrope](https://github.com/sharanda/manrope)         | SIL Open Font License 1.1, © The Manrope Project Authors   |
| [JetBrains Mono](https://github.com/JetBrains/JetBrainsMono) | SIL Open Font License 1.1, © The JetBrains Mono Project Authors |
