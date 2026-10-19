# forgeai

forgeai is a Kubernetes-native AI platform covering development, training, RL and inference. This first milestone covers **distributed training**.

| Component | Path | Description |
|---|---|---|
| Operator | `api/`, `internal/`, `cmd/` | `TrainingJob` CRD (short name `ftj`) on Kubeflow Trainer v2 (`TrainJob` + per-job `TrainingRuntime`) with Kueue admission; image warmup cleanup |
| Compat API | `compat-api/` | [huma](https://github.com/danielgtaylor/huma) service compatible with scc-k8s-v2 (jobs, images, volumes, resources) for third-party integration |
| GUI | `gui/` | [Wails](https://wails.io) desktop client that talks to Kubernetes directly through a kubeconfig context |
| Shared library | `pkg/` | Naming (scc-k8s-v2 compatible hashes), platform config, jobs, volumes, capacity, images |

## TrainingJob

```yaml
apiVersion: forgeai.wutz.dev/v1alpha1
kind: TrainingJob
metadata:
  name: p-97a5af344957-demo   # p-{fnv1a(ns-userID)[:12]}-{jobID}, same as scc-k8s-v2
  namespace: pod-prod
spec:
  userID: NMPHONE-xxxx
  jobID: demo
  image: pytorch/pytorch:2.10.0-cuda12.8-cudnn9-runtime
  command: torchrun train.py
  resourceID: rtx5090.xeon6530.xlarge   # from the platform config
  workers: 2
```

The operator renders:

- a `TrainingRuntime` and a `TrainJob`, queued through Kueue with LocalQueue `forgeai-queue`

Workers expose no endpoints: there is no SSH, Jupyter or exec access, because the pod network is not isolated between users. Use logs and mounted volumes instead.

Lifecycle:

| Action | How |
|---|---|
| Stop | `spec.suspend: true` |
| Start | `spec.suspend: false` |
| Restart | bump `spec.restartGeneration` |

`status` reports phase, workers, and pod and node placement.

Platform settings live in the ConfigMap `forgeai-system/forgeai-platform` (`config.yaml`): resource models, storage types, registry, volume usage endpoint, image warmup, queue and multi-node RDMA. See `deploy/dev/platform/config.yaml`.

## Compat API

The compat API serves the scc-k8s-v2 paths with the same request and response shapes:

```
POST   /apis/job/v1alpha2/users/{userID}/jobs?jobID=&method=create|start
GET    /apis/job/v1alpha2/users/{userID}/jobs[?jobID=]
GET    /apis/job/v1alpha2/users/{userID}/jobs/{jobID}
POST   /apis/job/v1alpha2/users/{userID}/jobs/{jobID}/stop | /restart
DELETE /apis/job/v1alpha2/users/{userID}/jobs/{jobID}

POST   /apis/pod/v1alpha2/images/warmup                       [{"id": "<image>"}]
GET    /apis/pod/v1alpha2/users/{userID}/images               Harbor project p-{userHash}

POST   /apis/pod/v1alpha2/users/{userID}/volumes?volumeID=    {"sizeGB", "type"}
GET    /apis/pod/v1alpha2/users/{userID}/volumes[?volumeID=]
GET    /apis/pod/v1alpha2/users/{userID}/volumes/{volumeID}
DELETE /apis/pod/v1alpha2/users/{userID}/volumes/{volumeID}   409 while a job or pod uses it
POST   /apis/pod/v1alpha2/users/{userID}/volumes/{volumeID}/resize
GET    /apis/pod/v1alpha2/users/{userID}/volumes/{volumeID}/usage

GET    /apis/pod/v1alpha2/resources[?include_details=]
GET    /apis/pod/v1alpha2/resources/{resourceID}
```

Differences from scc-k8s-v2:

- No SSH, Jupyter, port-forward or development container (pod) endpoints.
- No node pools: no pool or pool request endpoints, and jobs have no `pool_id`. Jobs are placed by resource model labels and Kueue quota; availability covers all nodes.
- Image warmup creates one DaemonSet per image. The operator deletes it once every selected node has the image or `imageWarmup.ttl` expires.
- Resource availability is computed live from node allocatable minus pod requests, with no Redis cache. `usageFrom` replaces the hard-coded 4c16g duplication.

- Auth: `x-api-key` header on every `/apis/` route except the docs. Accepted keys are taken from every `APIKEY*` env var.
- Optional env: `REGISTRY_USER`, `REGISTRY_PASSWORD` (image list) and `VOLUME_USAGE_TOKEN` (volume usage), from the secret `forgeai-compat-api-credentials`.
- Also served under the `/ngc` prefix.
- OpenAPI: `/apis/openapi.json`. Docs: `/apis/docs`.

## Development

```sh
make test                      # operator + envtest
(cd compat-api && go test ./...)
make gui                       # gui/build/bin/forgeai.app (needs a wails CLI built with golang.org/x/tools >= v0.52 first on PATH)
```

## Deploy (dev cluster)

```sh
kubectl create ns forgeai-system
kubectl -n forgeai-system create secret generic forgeai-compat-api-keys --from-literal=APIKEY=<key>
make deploy-dev                # ko build -> ghcr.io/wutz/forgeai (public), kubectl apply
```

Prerequisites:

- Kubeflow Trainer v2
- Kueue, with the `trainjob` integration enabled
