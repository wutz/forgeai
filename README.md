# forgeai

forgeai is a Kubernetes-native AI platform covering development, training, RL and inference. This first milestone covers **multi-tenant distributed training**: projects are namespaces with quotas, network isolation and member credentials, and jobs are plain Kubeflow Trainer v2 `TrainJob`s admitted by Kueue.

| Component | Path | Description |
|---|---|---|
| Operator | `api/`, `internal/`, `cmd/` | `FileServer` and `InferenceService` CRDs; adds stop/resume, failure classification and retries to `TrainJob`s; purges trashed volumes |
| Project operator | [wutz/project-operator](https://github.com/wutz/project-operator) | Separate repository and Helm chart: the `Project` CRD and its namespaces, quotas, queues, network policies and members. Installed next to this operator |
| GUI | `gui/` | [Wails](https://wails.io) desktop client that talks to Kubernetes directly through a kubeconfig context |
| Website | `website/` | Official site ([Astro](https://astro.build)), deployed to Cloudflare Workers at https://forgeai.wutz.dev |
| Shared library | `pkg/` | Naming, platform config, instance types, job rendering and status, sandboxes, volumes, Harbor client, kubeconfig export |

There is no API server of its own: the operator reconciles Kubernetes objects and the GUI (or `kubectl`) creates them with the member's own credentials.

## Projects

A `Project` (cluster-scoped, short name `fproj`) is a tenant. Its CRD and controller live in [wutz/project-operator](https://github.com/wutz/project-operator), which is installed separately (see [Helm charts](#helm-charts)). forgeai relies on these objects that the project operator creates:

| Object | Name | Used by |
|---|---|---|
| Namespace | `<namespacePrefix><project>` (e.g. `forge-demo`), labelled `forgeai.wutz.dev/project` | Every namespaced view. The GUI only accepts namespaces with that label |
| Kueue LocalQueue | `default` in the project namespace | Training jobs are submitted to it |
| ClusterRole | `forgeai:project:<project>` | Members' read access to the Project, its ClusterQueue, InstanceTypes and ClusterTrainingRuntimes |
| Member ServiceAccount | `forgeai-members/<project>.<member>` | Member kubeconfigs exported from the Projects view |
| Harbor project | named like the namespace, e.g. `forge-demo` | Images view |

Admins manage projects and members from the GUI's Projects view. What the Project controller creates, how quotas and network isolation work, and how deletion is handled are documented in the project-operator README.

## InstanceType

`InstanceType`s (cluster-scoped, short name `fit`) and the Kueue ResourceFlavors behind them are provided by a separate project, [forgeai-instancetype-operator](https://github.com/wutz/forgeai-instancetype-operator), which has its own Helm chart and release. forgeai only reads them: the GUI lists instance types to offer GPU classes and sizes, and the member RBAC grants read access to them. Install that operator's chart before using instance types.

```sh
kubectl get fit
```

## Training jobs

Jobs are Kubeflow Trainer v2 `TrainJob`s in the project namespace, submitted to the LocalQueue `default`. The GUI renders them from a form (`pkg/training`):

- The runtime is a job template (a namespaced `TrainingRuntime` labelled `forgeai.wutz.dev/template`) or the platform `training.defaultRuntime` `ClusterTrainingRuntime`.
- Image, command (a bash script run on every worker), env, worker count and per-worker resources of the instance type are trainer overrides; `numProcPerNode` is the GPU count.
- Node selector, GPU toleration, volume mounts, a `/dev/shm` of `training.shmSize` and the registry pull secret are a runtime patch owned by forgeai.

Workers expose no endpoints: there is no SSH, Jupyter or exec access. Use logs and mounted volumes instead.

The operator adds what Trainer and Kueue lack, for `TrainJob`s in project namespaces:

| Feature | How |
|---|---|
| Stop | annotation `forgeai.wutz.dev/stopped: "true"`: the Kueue Workload is deactivated, pods are released and the job is kept |
| Start | remove the annotation: the Workload is reactivated |
| Rerun | the GUI submits a new job from the form of an existing one |
| Stuck detection | a job whose pods cannot start (image pull errors, crash loops) for `training.stuckTimeout` is stopped and marked failed |
| Failure classification | `forgeai.wutz.dev/failure-reason`: `NodeFailure`, `Evicted`, `OOMKilled`, `ImagePullFailed`, `ConfigError`, `ExitCode` or `Unknown`, with `failure-message` and an event |
| Retries | a failed job is resubmitted as `<group>-r<n>` up to `forgeai.wutz.dev/max-retries` (default `training.maxRetries`). `retry-on: Infra` (default) retries `NodeFailure` and `Evicted`; `Any` retries everything except `ImagePullFailed` and `ConfigError` |

Retries share the `forgeai.wutz.dev/job-group` label; the GUI nests them under the first job.

## Sandboxes

Members run agents and other interactive workloads in [agent-sandbox](https://github.com/kubernetes-sigs/agent-sandbox) `Sandbox`es (SIG Apps, `agents.x-k8s.io/v1beta1`). forgeai does not reimplement the sandbox controller: it adds project RBAC, platform defaults, the stop/start contract and an idle policy.

```yaml
apiVersion: agents.x-k8s.io/v1beta1
kind: Sandbox
metadata:
  name: agent
  namespace: forge-demo
  labels:
    forgeai.wutz.dev/project: demo
spec:
  operatingMode: Running          # Suspended keeps the object, deletes the pod
  podTemplate:
    spec:
      runtimeClassName: gvisor    # isolation; kata also offered
      containers:
      - name: sandbox             # the container name forgeai renders
        image: cr.dev1.../forge-demo/agent:latest
```

The GUI renders the same object from a form (`pkg/sandbox`): image, bash command, env, isolation runtime class, an optional instance type (GPU, CPU/memory and node placement), project volumes and an auto-stop timer. `service: true` gives the sandbox a stable Service and DNS name.

| Feature | How |
|---|---|
| Stop | annotation `forgeai.wutz.dev/stopped: "true"`; the operator sets `operatingMode: Suspended`, the pod is deleted, the object and its volumes are kept |
| Start | remove the annotation; the sandbox comes back with its volumes |
| Idle suspend | a sandbox nothing used for its idle timeout is suspended and marked stopped. The timeout is the form's "Idle timeout" in minutes (0 turns the idle stop off for that sandbox, empty uses `sandbox.idleTimeout`), stored in the `forgeai.wutz.dev/idle-timeout` annotation; clients keep it awake with the `forgeai.wutz.dev/last-active` heartbeat (the GUI sends one while the sandbox is open) |
| Auto-stop | the form's TTL sets `shutdownTime` with `shutdownPolicy: Retain`, so the sandbox stops instead of being deleted |
| Templates | `SandboxTemplate`s are listed by the GUI; members create templates, claims and warm pools with `kubectl` (the edit role allows it) |
| Gateway access | the annotation `forgeai.wutz.dev/gateway-port: "8080"` (the form's `gatewayPort`) publishes the sandbox's HTTP port through the Gateway API: the operator owns a ClusterIP Service `<name>-gw` selecting the sandbox pod on that port and an HTTPRoute for host `<name>-<namespace>.<ingress domain>` on the platform's `gateway`. Clearing the annotation removes both |

Members create, stop, start and delete sandboxes in their namespace with the `forgeai-project-edit` role; `view` members see them read-only. Edit members also open a shell in a running sandbox from the GUI terminal tab (`pods/exec` on the sandbox container, with their own credentials). Project network policies, created by project-operator, admit the agent-sandbox controller in `sandbox.namespace`, so sandbox API servers reachable from the pod stay reachable in isolated projects.

### SSH

A sandbox labelled `forgeai.wutz.dev/ssh: "true"` (the create form's "SSH" switch) gets an sshd sidecar and a Service on port 22. An SSH session reaches only what the sidecar mounts: the project volumes it shares with the agent, not the agent's container image or its processes.

Entry is through sshpiperd. The [SSH operator](https://github.com/wutz/ssh-operator) runs it as an SSH gateway modelled on the Gateway API; the sshd sidecar of each sandbox stays in forgeai:

```yaml
apiVersion: forgeai.wutz.dev/v1alpha1
kind: SSHGateway
metadata:
  name: main
  namespace: forgeai-system
spec:
  gatewayClassName: forgeai-ssh
  listeners:
    - name: ssh
      port: 2222
      allowedRoutes: {namespaces: {from: All}}
---
apiVersion: forgeai.wutz.dev/v1alpha1
kind: SSHRoute
metadata:
  name: agent-login
  namespace: forge-demo
spec:
  parentRefs: [{name: main}]
  rules:
    - matches: [{username: alice}]
      authorizedKeys: ["ssh-ed25519 AAAA... alice@example.com"]
      backendRefs: [{name: agent}]
```

`SSHGatewayClass` names the controller (`forgeai.wutz.dev/ssh-gateway`). Only the gateway of the platform namespace is served, and only one of them exists: a second one there is rejected. Each rule of a route becomes one sshpiperd `Pipe` that accepts the keys of `authorizedKeys` for the listed usernames and forwards to the `SSHService` of the named sandbox, logging in as `sandbox` with the platform identity key.

- The gateway needs the sshpiper `Pipe` CRD (`pipes.sshpiper.com`, served by the sshpiper kubernetes plugin) installed in the cluster. The Dependencies view installs it as the `sshpiper` component, before the SSH operator. Without it the SSH operator starts without its gateway controllers, and the SSHGateway and SSHRoute stay without status.
- A username is claimed by the first route that matches it; keep login names unique.
- Keys are the only way in. Passwords, keyboard interaction, agent and TCP forwarding, tunnels and X11 are all off, at the gateway and at the sidecar.
- The gateway host key is generated once per gateway and kept in the Secret `forgeai-system/sshgateway-main-hostkey`; pin it with `known_hosts` rather than trusting on first use.
- The sshd sidecar image is `sandbox.sshImage` (default `ghcr.io/wutz/forgeai-sshd:0.1.0`, built by `make sshd-image`); sshpiperd is the `gateway.image` value of the SSH operator chart (default `ghcr.io/tg123/sshpiperd:v1.6.1`).

Route conditions are reported in `status.parents` (`Accepted`, `ResolvedRefs`); gateway conditions in `status.conditions` (`Accepted`, `Programmed`, `Ready`).

## Volumes

Volumes are PVCs in the project namespace, created from the platform `storageTypes` (ReadWriteMany by default) and limited by the project quota. Volumes can be resized (grow only) and moved to the trash:

- Trashing sets the label `forgeai.wutz.dev/trash` and `forgeai.wutz.dev/expire-at`; volumes mounted by running pods cannot be trashed, except by the file server: it is rebuilt without trashed volumes.
- Trashed volumes can be restored, purged, or have their expiry changed until they expire.
- The operator purges expired volumes, postponing while a pod still mounts them. A volume trashed without an expiry (e.g. labelled with `kubectl`) expires after the project `trashRetention` (default: platform `trashRetention`).

## FileServer

A `FileServer` (short name `ffs`) runs [dufs](https://github.com/sigoden/dufs) on demand to browse and transfer the files of the project's volumes:

```yaml
apiVersion: forgeai.wutz.dev/v1alpha1
kind: FileServer
metadata:
  name: default
  namespace: forge-demo
spec:
  volumes: []        # empty serves every ReadWriteMany volume not in the trash
  idleTimeout: 30m   # defaults to fileServer.idleTimeout
```

The GUI creates the `default` server on first use of the Files view and keeps it awake by updating the `forgeai.wutz.dev/last-active` annotation. The operator also reads the dufs access log for activity and scales the Deployment to zero after `idleTimeout`. Each volume is served under `/<name>` at `https://files-<namespace>.<ingress domain>[:port]`, with basic auth from the Secret `forgeai-fileserver`.

## Platform config

Platform settings live in the ConfigMap `forgeai-system/forgeai-platform` (`config.yaml`), see `pkg/platform/config.go` for defaults and `deploy/dev/platform/config.yaml` for an example:

| Key | Description |
|---|---|
| `namespacePrefix` | Prefix of project namespaces (default `forge-`) |
| `apiServer` | API server URL written into member kubeconfigs |
| `membersNamespace` | Namespace of member ServiceAccounts (default `forgeai-members`) |
| `ingress` | Ingress class, wildcard domain, external port and optional TLS secret for the file server |
| `gateway` | Gateway API Gateway the published sandbox routes attach to (`name`, `namespace`, optional `sectionName`); its listeners must admit routes from the project namespaces (`allowedRoutes`). `scheme` and `port` describe the listener from outside, for the URLs. The gateway does not authenticate published sandboxes: anything that reaches the host reaches the sandbox's port |
| `registry` | Harbor domain, API URL, `insecure`, and the admin Secret in `forgeai-system` (default `forgeai-harbor`, keys `username` and `password`) |
| `storageTypes`, `defaultStorageType` | Volume types mapped to StorageClasses |
| `instanceTypes` | Read by the InstanceType operator, not by forgeai: per-node `reserved` CPU and memory, and offered `sizes` (see its README) |
| `defaultQuota` | Default project quota: `gpus` per class (`"*"` for others), `storage`, `volumes`, `pods`, `headroom` |
| `network` | `systemNamespaces` allowed into isolated projects, `dnsNamespace` |
| `fileServer` | dufs `image`, `idleTimeout`, `hostPrefix` |
| `training` | `defaultRuntime`, `shmSize`, `stuckTimeout`, `maxRetries` |
| `sandbox` | agent-sandbox `namespace`, `defaultImage`, offered `images`, `runtimeClasses` and `defaultRuntimeClass`, `shmSize`, `idleTimeout` (default for sandboxes without their own; 0 disables), `sshImage` of the SSH sidecar |
| `trashRetention` | Default retention of trashed volumes |
| `templates` | `source` of the public template source the create dialogs offer: an `https://` base URL, a `file://` directory with `index.yaml`, or an `oci://` reference to a published source (default: [wutz/forgeai-templates](https://github.com/wutz/forgeai-templates) on its `main` branch) |

The forgeai operator rereads the config every 15 seconds. project-operator reads the project keys (`namespacePrefix`, `registry`, `ingress`, `storageTypes`, `defaultQuota`) from the same ConfigMap and reloads it when it changes.

## GUI

The desktop client connects with a kubeconfig context and a project namespace (the context namespace by default) and only accepts namespaces labelled as forgeai projects. Views:

| View | Description |
|---|---|
| Jobs | Create, stop, start, rerun and delete jobs; worker status and logs; retries nested under their job |
| Sandboxes | Create, stop, start and delete agent sandboxes; logs, endpoint, resources and idle state (shown when the cluster runs agent-sandbox) |
| Templates | Sandbox templates (prefill the sandbox form) and job templates (`TrainingRuntime`s, prefill the job form) in one view; create and delete both, each sorted by name |

| Volumes | Create, resize, trash, restore and purge volumes |
| Files | Opens the on-demand file server |
| Images | Push instructions and robot credentials; list and delete images in the project's Harbor project. Their tags are offered in a dropdown as the image of jobs, sandboxes and their templates, which also accept any other address |
| Resources | Instance types with availability and the project quota usage |
| Projects | Admins only (may create Projects): manage projects and members, export member kubeconfigs |
| Dependencies | Cluster admins only: install or upgrade Kueue, Kubeflow Trainer v2 (with JobSet) and agent-sandbox at the versions pinned in the GUI, install the forgeai operator from its Helm chart at the pinned chart version, install the project-operator Helm chart at its pinned version, install the sshpiper Pipe CRD and the SSH operator from its Helm chart (see [SSH](#ssh)), and see each component's status |

The New job and New sandbox dialogs also offer template sources: job and sandbox templates from the platform's `templates.source` (default [wutz/forgeai-templates](https://github.com/wutz/forgeai-templates)), grouped apart from the project templates. You can add other published template sources by URL, `file://` directory or `oci://` reference (`oci://registry/repository:tag` or `oci://registry/repository@sha256:…`) under **Template sources** in the sidebar; a source is read before it is added, and its templates are marked as yours in the dialog. Picking a template prefills the form; the name, instance type and volumes stay yours, and the job or sandbox does not reference the template.

View-only members see the same views read-only and get no registry or file server credentials.

## Development

```sh
make test                      # operator unit tests with envtest (Trainer and Kueue CRDs from internal/controller/testdata)
make lint
make test-e2e                  # against a dedicated Kind cluster
make gui                       # gui/build/bin/forgeai.app (needs a wails CLI built with golang.org/x/tools >= v0.52 first on PATH)
make gui-dev                   # wails dev; keeps the packaged app intact (plain `wails dev` deletes its executable)
```

After editing `api/v1alpha1/*_types.go` or kubebuilder markers, run `make manifests generate`.

## Deploy (dev cluster)

```sh
kubectl create ns forgeai-system
kubectl -n forgeai-system create secret generic forgeai-harbor \
  --from-literal=username=admin --from-literal=password=<password>
make deploy-dev                # ko build -> ghcr.io/wutz/forgeai (public), kubectl apply --server-side
```

`deploy/dev` combines `config/default` with the platform ConfigMap. It does not include the Project CRD or controller: install project-operator for Projects (see below). `make render-dev` only renders `dist/dev.yaml`; `make undeploy-dev` removes everything, including the CRDs.

### Helm charts

forgeai ships as the Helm chart in `charts/chart`: the FileServer and InferenceService CRDs, RBAC and the controller Deployment, with the platform config under `platform.config`. Projects come from a second chart, [project-operator](https://github.com/wutz/project-operator). The Dependencies view installs both at their pinned versions, as releases `forgeai` and `project-operator` in `forgeai-system`. The same installs from the CLI, in this order:

```sh
helm install forgeai oci://ghcr.io/wutz/charts/forgeai --version 0.4.0 \
  --namespace forgeai-system --create-namespace
helm install project-operator oci://ghcr.io/wutz/charts/project-operator --version 0.4.1 \
  --namespace forgeai-system
```

project-operator must run in the namespace of forgeai: it reads the platform ConfigMap from its own namespace (`FORGEAI_NAMESPACE`).

Releases up to 0.3.1 installed the `projects.forgeai.wutz.dev` CRD as part of the forgeai release. Upgrading keeps that CRD and its Projects, because the CRD carries `helm.sh/resource-policy: keep`. While forgeai owns the CRD, Helm refuses to install project-operator (`invalid ownership metadata`). Hand the CRD to the new release before installing it:

```sh
kubectl annotate crd projects.forgeai.wutz.dev \
  meta.helm.sh/release-name=project-operator \
  meta.helm.sh/release-namespace=forgeai-system --overwrite
kubectl label crd projects.forgeai.wutz.dev app.kubernetes.io/managed-by=Helm --overwrite
```

Pushing a `v<version>` tag runs `.github/workflows/helm-release.yml`, which publishes the operator image (`ghcr.io/wutz/forgeai:<version>`) and the chart (`oci://ghcr.io/wutz/charts/forgeai:<version>`). `make helm-lint` lints and renders the chart; `make helm-package CHART_VERSION=<version>` writes the package to `dist/`.

The SSH operator is a separate project, [wutz/ssh-operator](https://github.com/wutz/ssh-operator), published as the chart `oci://ghcr.io/wutz/charts/ssh-operator`. The Dependencies view installs the sshpiper `Pipe` CRD, then the pinned chart version (0.1.0) as release `ssh-operator` in `ssh-operator-system`, with `platform.namespace` set to the platform namespace. The same install from the CLI, once the Pipe CRD is in the cluster:

```sh
helm install ssh-operator oci://ghcr.io/wutz/charts/ssh-operator --version 0.1.0 \
  --namespace ssh-operator-system --create-namespace \
  --set platform.namespace=forgeai-system
```

A cluster that ran the SSH gateway from an earlier forgeai release has the `SSHGatewayClass`, `SSHGateway` and `SSHRoute` CRDs of `forgeai.wutz.dev` installed without Helm, and Helm does not adopt them. Back up their objects first (`kubectl get sshgatewayclasses,sshgateways,sshroutes -A -o yaml > ssh-backup.yaml`), delete the three CRDs, install the chart, and apply the backup again. This path is not verified on a cluster that ran the earlier release.

A cluster set up with `make deploy-dev` runs the operator outside Helm. Helm does not adopt objects it did not create, so the Dependencies view reports that operator as deployed outside Helm and refuses the chart install. Move such a cluster by removing the dev deployment with `make undeploy-dev` (this deletes the forgeai CRDs and all forgeai objects, so back them up first) and then installing the charts.

Prerequisites (the Dependencies view installs Kubeflow Trainer, agent-sandbox, Kueue, the sshpiper Pipe CRD and the SSH operator into a cluster; `make gui-deps` refreshes the pinned manifests embedded in the GUI):

- Kubeflow Trainer v2 (with JobSet) and a `ClusterTrainingRuntime` named after `training.defaultRuntime`
- agent-sandbox v1.0 or later (CRDs `agents.x-k8s.io/v1beta1`), for the Sandboxes view; the isolation RuntimeClasses (`gvisor`, `kata`) come from the node runtimes
- Kueue, with the `trainer.kubeflow.org/trainjob` integration enabled (the bundled Kueue config enables it)
- NVIDIA GPU operator (GPU feature discovery labels and the `nvidia.com/gpu` resource); not installed from the GUI
- A CNI that enforces NetworkPolicies, an ingress controller and Harbor
- For SSH sandboxes only: the sshpiper `Pipe` CRD and the [SSH operator](https://github.com/wutz/ssh-operator), see [SSH](#ssh)
