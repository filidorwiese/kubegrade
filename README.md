# kubegrade

Grades a Kubernetes cluster from your kubeconfig. Read-only, one scan per
run, nothing about the cluster leaves your machine. Version data comes from
endoflife.date and Artifact Hub.

```sh
curl -sL https://github.com/filidorwiese/kubegrade/releases/latest/download/kubegrade_$(uname -s | tr A-Z a-z)_$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/') -o kubegrade && chmod +x kubegrade
```

Or `go install github.com/filidorwiese/kubegrade/cmd/kubegrade@latest`.

```sh
kubegrade                 # current context
kubegrade -v              # include info findings
kubegrade --format json
```

Flags: `--kubeconfig`, `--cluster-name`, `--format text|json`, `-v`, `--no-color`.
Needs cluster-wide read access, including Helm release secrets.

```
GRADE  B   (84)   capped by: Hygiene

VERSIONS  89
  medium  helm traefik/traefik    major 41.6.0 available, have 40.3.0  major upgrade, read the changelog first
  low     helm kube-system/kured  1 minor behind 6.1.0, have 6.0.0     helm upgrade to 6.1.0
  2 info hidden, -v to show

HYGIENE  81
  medium  deploy shop/checkout    image checkout-api:latest            pin a version tag
  low     node worker2            kernel 6.12.63 differs from 4 nodes on 6.12.107  pending reboot or upgrade

HEALTH  83
  medium  deploy shop/plausible   CrashLoopBackOff 1/1 pods, 5 restarts (exit 1)  kubectl logs -p

LINKS
  helm traefik/traefik: https://github.com/traefik/traefik-helm-chart
```

Three categories, 100 points each. Findings deduct low 3, medium 8, high 15,
critical 30. Overall is the average, never better than the worst category.
A+ 95, A 85, B 70, C 55, D 40, else F.

- **Versions**: Kubernetes, kernel and OS support windows, kubelet skew, Helm charts vs upstream.
- **Hygiene**: deprecated APIs, `:latest` tags, missing digests, Helm revision pile-up, node drift.
- **Health**: crash loops, frequent restarts, pending pods, unavailable deployments, failed releases, NotReady nodes, full volumes.

Charts are resolved on Artifact Hub by name; when several share a name the
release's home and source URLs pick the right one, otherwise the finding says
it was guessed.
