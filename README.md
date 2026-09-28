# kubegrade

Free, open-source CLI that scans a Kubernetes cluster through your kubeconfig
and prints findings plus a letter grade. Built for k3s, works on any cluster.
Read-only. It fetches public EOL tables from endoflife.date and Helm repo
`index.yaml` files; nothing about your cluster is sent anywhere.

See `PRD.md` for the decisions behind it.

## Run

```sh
go run ./cmd/kubegrade
go run ./cmd/kubegrade --format json
go run ./cmd/kubegrade --kubeconfig ~/.kube/other
```

One scan per invocation. Flags: `--format text|json`, `--cluster-name`
(defaults to the current context), `--kubeconfig`
(defaults to `$KUBECONFIG` or `~/.kube/config`).

`task build` puts a static binary in `bin/kubegrade`.

## Grading

Three categories of 100 points, each answering one question:

- **Versions**: Kubernetes, kernel, OS support windows; chart versions;
  kubelet skew.
- **Hygiene**: deprecated APIs, `:latest` tags, missing digests, Helm
  revision pile-up, drift between nodes.
- **Health**: crash loops, pending pods, unavailable deployments, failed
  Helm releases, NotReady nodes, full volumes.

Findings deduct info 0, low 3, medium 8, high 15, critical 30. Overall is the average, capped at the worst category's
letter, with the score clamped to the top of that band.

A+ 95-100, A 85-94, B 70-84, C 55-69, D 40-54, F 0-39.

## Data

Kubernetes, kernel and OS EOL dates come from endoflife.date at startup. If
that fetch fails the run exits with an error. Pluto's `versions.yaml`
(Apache-2.0, vendored unchanged apart from a header) is embedded;
`task refresh-data` re-vendors it.

Each Helm release is compared against its upstream chart version. Releases
don't record their repo, so charts are looked up on Artifact Hub by name.
When several packages share a name, the release's `home` and `sources` from
Chart.yaml pick the right one; failing that the official package, then the
best-ranked one is used and the finding says it was guessed.
`internal/data/charts.yaml` overrides the lookup for private repos or wrong
guesses. Charts not found anywhere show as info.

## Permissions

The kubeconfig user needs read access, cluster-wide, to: nodes, nodes/proxy,
pods, namespaces, persistentvolumeclaims, secrets, deployments, replicasets,
and list on whatever deprecated API groups the Pluto table names.
Cluster-admin covers it.

`secrets` is the sensitive one. The tool lists only secrets matching
`owner=helm` + `type=helm.sh/release.v1`, via label and field selectors in
the code. It decodes the release payload in memory and never prints secret
contents.

## Known limitations

- A single run cannot measure how long a CrashLoopBackOff or a full PVC has
  lasted. Severity uses restart count and fill percentage instead. Pending
  pods, unavailable deployments and NotReady nodes use real API timestamps.
- Single-node clusters make half the node checks trivially pass.
- Deprecated API detection relies on `managedFields` and the last-applied
  annotation; objects written by clients that set neither are invisible.
- Reboot-required, pending OS updates and node-local disk pressure need
  host access. Out of scope.
- No CVE data, no image tag lookups. OCI-hosted charts are not resolved.
- Flatcar is matched but has no EOL data on endoflife.date.
