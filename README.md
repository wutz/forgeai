# forgeai

forgeai is a Kubernetes-native AI platform covering development, training, RL and inference. This first milestone covers **distributed training**.

| Component | Path | Description |
|---|---|---|
| Operator | `api/`, `internal/`, `cmd/` | `TrainingJob` CRD (short name `ftj`) on Kubeflow Trainer v2 (`TrainJob` + per-job `TrainingRuntime`) with Kueue admission; `PoolRequest` CRD (`fpr`) for node pool transfers; image warmup cleanup |
| Compat API | `compat-api/` | [huma](https://github.com/danielgtaylor/huma) service compatible with scc-k8s-v2 (jobs, images, volumes, pools, resources) for third-party integration |
| GUI | `gui/` | [Wails](https://wails.io) desktop client that talks to Kubernetes directly through a kubeconfig context |
| Shared library | `pkg/` | Naming (scc-k8s-v2 compatible hashes), platform config, jobs, volumes, pools, capacity, images |

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

GET    /apis/pool/v1alpha2/pools
GET    /apis/pool/v1alpha2/{pool_id}/pods
POST   /apis/node/v1alpha2/users/{userID}/pools/requests?request_id=   {"src_pool", "dst_pool", "resourceID", "nnodes"}
GET    /apis/node/v1alpha2/users/{userID}/pools/requests/{request_id}
DELETE /apis/node/v1alpha2/users/{userID}/pools/requests/{request_id}
DELETE /apis/node/v1alpha2/users/{userID}/pools/{poolID}       nodes go back to "default"

GET    /apis/pod/v1alpha2/resources[?pool_id=&include_details=]
GET    /apis/pod/v1alpha2/resources/{resourceID}
```

Differences from scc-k8s-v2:

- No SSH, Jupyter, port-forward or development container (pod) endpoints.
- A pool request is a `PoolRequest`. The operator holds `nnodes` matching nodes of `src_pool` with a placeholder Job, relabels them to `dst_pool`, then releases the Job. This replaces the readiness callback.
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
make gui                       # gui/build/bin/forgeai.app (needs wails CLI built with a recent golang.org/x/tools)
```

## Deploy (dev cluster)

```sh
kubectl create ns forgeai-system
kubectl -n forgeai-system create secret generic forgeai-compat-api-keys --from-literal=APIKEY=<key>
make deploy-dev                # ko build -> ghcr.io/scclabs/forgeai (public), kubectl apply
```

Prerequisites:

- Kubeflow Trainer v2
- Kueue, with the `trainjob` integration enabled
