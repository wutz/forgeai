# forgeai

forgeai is a Kubernetes-native AI platform covering development, training, RL and inference. This first milestone covers **distributed training**.

| Component | Path | Description |
|---|---|---|
| Operator | `api/`, `internal/`, `cmd/` | `TrainingJob` CRD (`forgeai.scclabs.dev/v1alpha1`, short name `ftj`). Built on Kubeflow Trainer v2 (`TrainJob` + per-job `TrainingRuntime`), Kueue admission, and per-worker SSH (sshpiper), Jupyter (Ingress) and secrets |
| Compat API | `compat-api/` | [huma](https://github.com/danielgtaylor/huma) service compatible with scc-k8s-v2 `/apis/job/v1alpha2` for third-party integration |
| GUI | `gui/` | [Wails](https://wails.io) desktop client that talks to Kubernetes directly through a kubeconfig context |
| Shared library | `pkg/` | Naming (scc-k8s-v2 compatible hashes), platform config, job operations |

## TrainingJob

```yaml
apiVersion: forgeai.scclabs.dev/v1alpha1
kind: TrainingJob
metadata:
  name: p-97a5af344957-demo   # p-{fnv1a(ns-userID)[:12]}-{jobID}, same as scc-k8s-v2
  namespace: pod-prod
spec:
  userID: NMPHONE-xxxx
  jobID: demo
  poolID: ackpool-00000f0v
  image: pytorch/pytorch:2.10.0-cuda12.8-cudnn9-runtime
  command: torchrun train.py
  resourceID: rtx5090.xeon6530.xlarge   # from the platform config
  workers: 2
```

The operator renders:

- a `TrainingRuntime` and a `TrainJob`, queued through Kueue with LocalQueue `forgeai-queue`
- a per-job secret holding `JUPYTER_TOKEN` and `SSH_PASSWORD`
- for each worker: a Service, an Ingress, an SSH Service and an sshpiper `Pipe` (`root@{jobID}-{i}`)

Lifecycle:

| Action | How |
|---|---|
| Stop | `spec.suspend: true` |
| Start | `spec.suspend: false` |
| Restart | bump `spec.restartGeneration` |

`status` reports phase, workers, pod and node placement, SSH users and Jupyter URLs.

Platform settings live in the ConfigMap `forgeai-system/forgeai-platform` (`config.yaml`): resource models, domains, sshpiper, queue and multi-node RDMA. See `deploy/dev/platform/config.yaml`.

## Compat API

The compat API serves the scc-k8s-v2 paths with the same request and response shapes:

```
POST   /apis/job/v1alpha2/users/{userID}/jobs?jobID=&method=create|start
GET    /apis/job/v1alpha2/users/{userID}/jobs[?jobID=]
GET    /apis/job/v1alpha2/users/{userID}/jobs/{jobID}
PUT    /apis/job/v1alpha2/users/{userID}/jobs/{jobID}/stop | /restart
DELETE /apis/job/v1alpha2/users/{userID}/jobs/{jobID}
GET    /apis/job/v1alpha2/users/{userID}/jobs/{jobID}/ssh
```

- Auth: `x-api-key` header. Accepted keys are taken from every `APIKEY*` env var.
- Also served under the `/ngc` prefix.
- OpenAPI: `/apis/openapi.json`. Docs: `/apis/docs`.

## Development

```sh
make test                      # operator + envtest
(cd compat-api && go test ./...)
make gui                       # gui/build/bin/forgeai.app (needs wails CLI built with a recent golang.org/x/tools)
```

## Deploy (dev cluster)

```sh
kubectl create ns forgeai-system
kubectl -n forgeai-system create secret docker-registry forgeai-registry ...
kubectl -n forgeai-system create secret generic forgeai-compat-api-keys --from-literal=APIKEY=<key>
make deploy-dev                # ko build -> cr.dev1.bz1.paratera.com:7443/forgeai, kubectl apply
```

Prerequisites:

- Kubeflow Trainer v2
- Kueue, with the `trainjob` integration enabled
- sshpiper (CRD `pipes.sshpiper.com`)
- ingress-nginx
