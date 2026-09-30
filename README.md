# Kubegrade

Grades the upkeep of your Kubernetes cluster from A+ to F.

![example report](kubegrade-example-report.png)

Kubegrade CLI-tool scans your Kubernetes cluster for these upkeep issues:

- **Versions**: Kubernetes, kernel and OS versions and support windows, kubelet skew, Helm charts vs upstream, deprecated charts.
- **Hygiene**: deprecated APIs, `:latest` tags, missing digests, Helm revision pile-up, node drift, cordoned nodes, skipped reboots, leftover pods, unmounted volumes, expired unused TLS secrets.
- **Health**: crash loops, frequent restarts, image pull failures, pending pods, unavailable deployments, failed releases, NotReady nodes, node pressure, full disks and volumes, expiring certificates.

Every finding deducts points by severity. The overall grade is capped by the
weakest category.

Note: nothing about the cluster leaves your machine; version data comes from
[endoflife.date](https://endoflife.date) and [Artifact Hub](https://artifacthub.io) and [Docker Hub](https://hub.docker.com).

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/filidorwiese/kubegrade/main/install.sh | sh
```

Verifies the checksum and installs to /usr/local/bin, or ~/.local/bin when
that is not writable. Rerun it to update.

Reads $KUBECONFIG or ~/.kube/config. Needs cluster-wide read access.
Missing permissions skip a check and are listed in the report.

## Usage

```sh
kubegrade                 # scan the current context of $KUBECONFIG or ~/.kube/config
kubegrade -v              # include info findings
kubegrade -vv             # also list checks that passed
kubegrade --format json
kubegrade version
```

Flags: `--context`, `--format text|json`, `-v`, `-vv`, `--no-color`.

Licensed under Apache-2.0.
