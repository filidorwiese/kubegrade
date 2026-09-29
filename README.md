# kubegrade

![example report](kubegrade-example-report.png)

Grades the upkeep of your Kubernetes cluster from A+ to F. It scans read-only
from your kubeconfig and suggests a fix for each finding. Nothing about the
cluster leaves your machine; version data comes from
[endoflife.date](https://endoflife.date) and [Artifact Hub](https://artifacthub.io).

- **Versions**: Kubernetes, kernel and OS support windows, kubelet skew, Helm charts vs upstream, deprecated charts.
- **Hygiene**: deprecated APIs, `:latest` tags, missing digests, Helm revision pile-up, node drift, cordoned nodes, leftover pods, unmounted volumes, expired unused TLS secrets.
- **Health**: crash loops, frequent restarts, image pull failures, pending pods, unavailable deployments, failed releases, NotReady nodes, node pressure, full disks and volumes, expiring certificates.

Every finding deducts points by severity. The overall grade is capped by the
weakest category.

## Install

```sh
curl -sL https://github.com/filidorwiese/kubegrade/releases/latest/download/kubegrade_$(uname -s | tr A-Z a-z)_$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/') -o kubegrade && chmod +x kubegrade
```

Reads $KUBECONFIG or ~/.kube/config. Needs cluster-wide read access.
Missing permissions skip a check and are listed in the report.

## Usage

```sh
kubegrade                 # scan the current context of $KUBECONFIG or ~/.kube/config
kubegrade -v              # include info findings
kubegrade -vv             # also list checks that passed
kubegrade --format json
kubegrade update          # replace the binary with the latest release
kubegrade version
```

Flags: `--context`, `--format text|json`, `-v`, `-vv`, `--no-color`.

Licensed under Apache-2.0.
