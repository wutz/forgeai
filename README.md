# forgeai

forgeai is a Kubernetes-native AI platform covering development, training, RL and inference. This first milestone covers **multi-tenant distributed training**: projects are namespaces with quotas, network isolation and member credentials, and jobs are plain Kubeflow Trainer v2 `TrainJob`s admitted by Kueue.

| Component | Path | Description |
|---|---|---|
| Operator | `api/`, `internal/`, `cmd/` | `InferenceService` CRD; adds stop/resume, failure classification and retries to `TrainJob`s; purges trashed volumes |
| Workspace operator | [wutz/workspace](https://github.com/wutz/workspace) | Separate repository and Helm chart: the `Project` and `InstanceType` CRDs (`workspace.wutz.dev`), their namespaces, quotas, queues, network policies, members and the InstanceTypes derived from GPU nodes. Installed before this operator |
| FileServer operator | [wutz/fileserver](https://github.com/wutz/fileserver) | Separate repository and Helm chart: the `FileServer` CRD and the on-demand file servers of project volumes. Installed next to this operator |
| GUI | `gui/` | [Wails](https://wails.io) desktop client that talks to Kubernetes directly through a kubeconfig context |
| Website | `website/` | Official site ([Astro](https://astro.build)), deployed to Cloudflare Workers at https://forgeai.wutz.dev |
| Shared library | `pkg/` | Naming, platform config, instance types, job rendering and status, sandboxes, volumes, Harbor client, kubeconfig export |

There is no API server of its own: the operator reconciles Kubernetes objects and the GUI (or `kubectl`) creates them with the member's own credentials.

## Projects

A `Project` (cluster-scoped, short name `fproj`, API group `workspace.wutz.dev`) is a tenant. Its CRD and controller live in [wutz/workspace](https://github.com/wutz/workspace), which is installed separately (see [Helm charts](#helm-charts)). forgeai relies on these objects that the workspace operator creates:

| Object | Name | Used by |
|---|---|---|
| Namespace | `<namespacePrefix><project>` (e.g. `forge-demo`), labelled `workspace.wutz.dev/project` | Every namespaced view. The GUI only accepts namespaces with that label |
| Kueue LocalQueue | `default` in the project namespace, when Kueue is installed | Training jobs are submitted to it |
| ClusterRole | `project:project:<project>` | Members' read access to the Project, its ClusterQueue, InstanceTypes and ClusterTrainingRuntimes |
| Member ServiceAccount | `project-members/<project>.<member>` | Member kubeconfigs exported from the Projects view |
| Harbor project | named like the namespace, e.g. `forge-demo` | Images view |

This operator adds its own kinds to the workspace roles: the `forgeai-member-edit` and `forgeai-member-view` ClusterRoles grant InferenceServices and are aggregated into the workspace operator's `workspace-edit` and `workspace-view` through the label `workspace.wutz.dev/aggregate-to-member`.

Admins manage projects and members from the GUI's Projects view. What the Project controller creates, how quotas and network isolation work, and how deletion is handled are documented in the project README.

## InstanceType

`InstanceType`s (`workspace.wutz.dev`, cluster-scoped, short name `fit`) are derived from GPU nodes by the workspace operator, together with the Kueue ResourceFlavors behind them when Kueue is installed. forgeai only reads them: the GUI lists instance types to offer GPU classes and sizes, and the member RBAC grants read access to them. Install the project chart before using instance types.

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
    workspace.wutz.dev/project: demo
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

Members create, stop, start and delete sandboxes in their namespace with the `workspace-edit` role (and `forgeai-member-edit` for InferenceServices); `view` members see them read-only through `workspace-view`. Edit members also open a shell in a running sandbox from the GUI terminal tab (`pods/exec` on the sandbox container, with their own credentials). Project network policies, created by the workspace operator, admit the agent-sandbox controller in `sandbox.namespace`, so sandbox API servers reachable from the pod stay reachable in isolated projects.

### SSH

A sandbox labelled `forgeai.wutz.dev/ssh: "true"` (the create form's "SSH" switch) gets a Service on port 22 and the Secret its sshd reads. The create form renders the sshd sidecar too, so its sandboxes are reachable at once; the operator wires the Service for the label alone, so a sandbox created with `kubectl` starts without a sidecar and needs one added to its pod template. An SSH session reaches only what the sidecar mounts: the project volumes it shares with the agent, not the agent's container image or its processes.

Entry is through sshpiper. The [SSH gateway operator](https://github.com/wutz/sshgateway) runs it as an SSH gateway modelled on the Gateway API. It is generic: a backend is any Service that exposes an sshd. The sshd sidecar of each sandbox, and the Service that exposes it, stay in forgeai:

```yaml
apiVersion: sshgateway.wutz.dev/v1alpha1
kind: SSHGateway
metadata:
  name: main
  namespace: forgeai-system
spec:
  gatewayClassName: sshgateway
  listeners:
    - name: ssh
      port: 2222
      allowedRoutes: {namespaces: {from: All}}
---
apiVersion: sshgateway.wutz.dev/v1alpha1
kind: SSHRoute
metadata:
  name: agent-login
  namespace: forge-demo
spec:
  parentRefs: [{name: main}]
  rules:
    - matches: [{username: alice}]
      authorizedKeys: ["ssh-ed25519 AAAA... alice@example.com"]
      backendRefs: [{name: agent, username: sandbox}]
```

`SSHGatewayClass` `sshgateway` names the controller (`sshgateway.wutz.dev/ssh-gateway`). Only the gateway of the platform namespace is served, and only one of them exists: a second one there is rejected. Each rule of a route becomes one sshpiper `Pipe` that accepts the keys of `authorizedKeys` for the listed usernames and forwards to the backend Service named by `backendRefs`, logging in as the ref's `username` with the platform identity key. For a sandbox the Service is the sandbox's SSH Service and the username is `sandbox`, the account its sshd allows.

- The gateway needs the sshpiper `Pipe` CRD (`pipes.sshpiper.com`, served by the sshpiper kubernetes plugin) installed in the cluster. The Dependencies view installs it as the `sshpiper` component, before the SSH gateway. Without it the SSH gateway operator starts without its gateway controllers, and the SSHGateway and SSHRoute stay without status.
- A username is claimed by the first route that matches it; keep login names unique.
- Keys are the only way in. Passwords, keyboard interaction, agent and TCP forwarding, tunnels and X11 are all off, at the gateway and at the sidecar.
- The gateway host key is generated once per gateway and kept in the Secret `forgeai-system/sshgateway-main-hostkey`; pin it with `known_hosts` rather than trusting on first use.
- The sshd sidecar image is `sandbox.sshImage` (default `ghcr.io/wutz/forgeai-sshd:0.1.0`, built by `make sshd-image`); sshpiper is the `gateway.image` value of the SSH gateway chart (default `ghcr.io/tg123/sshpiperd:v1.6.1`).

Route conditions are reported in `status.parents` (`Accepted`, `ResolvedRefs`); gateway conditions in `status.conditions` (`Accepted`, `Programmed`, `Ready`).

## Volumes

Volumes are PVCs in the project namespace, created from the platform `storageTypes` (ReadWriteMany by default) and limited by the project quota. Volumes can be resized (grow only) and moved to the trash:

- Trashing sets the label `fileserver.wutz.dev/ignore` and the annotation `forgeai.wutz.dev/expire-at`; the FileServer operator does not serve a volume with the ignore label. Volumes mounted by running pods cannot be trashed, except by the file server: it is rebuilt without trashed volumes. Volumes trashed before FileServer 0.3.0 carry `forgeai.wutz.dev/trash`; the Dependencies install moves that label to `fileserver.wutz.dev/ignore`.
- Trashed volumes can be restored, have their expiry changed or be manually purged until they expire.
- The volumetrash operator, installed from the Dependencies view, purges expired volumes, postponing while a pod still mounts them. A volume trashed without an expiry (e.g. labelled with `kubectl`) expires after the project `trashRetention` (default: platform `trashRetention`).

## FileServer

The file servers that browse and transfer the files of a project's volumes are run by [fileserver](https://github.com/wutz/fileserver), a separate project with its own Helm chart. It serves a `FileServer` (short name `ffs`) per namespace with [dufs](https://github.com/sigoden/dufs), scaled to zero when idle. forgeai only creates the `default` `FileServer` on first use of the Files view and keeps it awake by updating the `fileserver.wutz.dev/last-active` annotation the FileServer operator reads; the operator does the rest.

The Dependencies view installs the operator from `oci://ghcr.io/wutz/charts/fileserver` at the pinned chart version, as release `fileserver` in `forgeai-system`. Before 0.3.0 the release was `fileserver-operator`, and its CRD blocks the new release, so the install first uninstalls that release. This deletes its FileServers, and their Deployments, Services, Ingresses and Secrets with them; the `default` FileServer is created again on the next use of the Files view. Then the install moves the volumes trashed under `forgeai.wutz.dev/trash` to `fileserver.wutz.dev/ignore`, so the new operator does not serve them. It maps the platform config into the chart values: `ingress` (class, domain, port, TLS secret) and `fileServer` (dufs image, idle timeout, host prefix). Until the operator is installed, the Files view has no server to show.

The FileServer operator's API group is `fileserver.wutz.dev` from chart 0.2.0. Upgrading from forgeai 0.4.0 leaves the old `fileservers.forgeai.wutz.dev` CRD and its `FileServer` objects in place, because the forgeai chart stopped shipping the CRD and it carries the `helm.sh/resource-policy: keep` annotation. Once the FileServer operator is at 0.2.0, the Files view creates a new `default` `FileServer` in the new group; the old CRD is not used and can be deleted by hand. Upgrading the FileServer operator from 0.1.0 to 0.2.0 removes the old CRD and the `FileServer` objects it held. Uninstalling the FileServer operator chart deletes its CRD, and with it every `FileServer`.

## Platform config

Platform settings live in the ConfigMap `forgeai-system/forgeai-platform` (`config.yaml`). The forgeai chart renders the same settings into `forgeai-system/workspace-platform` for the workspace operator, which ignores the keys it does not use. Both operators fail to reconcile while the ConfigMap is missing, so the chart always renders one: its defaults set `namespacePrefix` and `registry.adminSecret` to the values below, which the workspace operator would otherwise default differently (`project-` and `project-registry-admin`). See `pkg/platform/config.go` for defaults and `deploy/dev/platform/config.yaml` for an example:

| Key | Description |
|---|---|
| `namespacePrefix` | Prefix of project namespaces (default `forge-`) |
| `apiServer` | API server URL written into member kubeconfigs |
| `ingress` | Ingress class, wildcard domain, external port and optional TLS secret for the file server |
| `gateway` | Gateway API Gateway the published sandbox routes attach to (`name`, `namespace`, optional `sectionName`); its listeners must admit routes from the project namespaces (`allowedRoutes`). `scheme` and `port` describe the listener from outside, for the URLs. The gateway does not authenticate published sandboxes: anything that reaches the host reaches the sandbox's port |
| `registry` | Harbor domain, API URL, `insecure`, and the admin Secret in `forgeai-system` (default `forgeai-harbor`, keys `username` and `password`) |
| `storageTypes`, `defaultStorageType` | Volume types mapped to StorageClasses |
| `instanceTypes` | Read by the workspace operator, not by forgeai: per-node `reserved` CPU and memory, and offered `sizes` (see its README) |
| `defaultQuota` | Default project quota: `gpus` per class (`"*"` for others), `storage`, `volumes`, `pods`, `headroom` |
| `network` | `systemNamespaces` allowed into isolated projects, `dnsNamespace` |
| `fileServer` | dufs `image`, `idleTimeout`, `hostPrefix` |
| `training` | `defaultRuntime`, `shmSize`, `stuckTimeout`, `maxRetries` |
| `sandbox` | agent-sandbox `namespace`, `defaultImage`, offered `images`, `runtimeClasses` and `defaultRuntimeClass`, `shmSize`, `idleTimeout` (default for sandboxes without their own; 0 disables), `sshImage` of the SSH sidecar |
| `trashRetention` | Default retention of trashed volumes |
| `templates` | `source` of the public template source the create dialogs offer: an `https://` base URL, a `file://` directory with `index.yaml`, or an `oci://` reference to a published source (default: [wutz/forgeai-templates](https://github.com/wutz/forgeai-templates) on its `main` branch) |

The forgeai operator rereads the config every 15 seconds. The workspace operator reads the project keys (`namespacePrefix`, `registry`, `ingress`, `storageTypes`, `defaultQuota`) from the same ConfigMap and reloads it when it changes.

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
| Dependencies | Cluster admins only: install or upgrade Kueue, Kubeflow Trainer v2 (with JobSet) and agent-sandbox at the versions pinned in the GUI, install the InstanceType operator, the forgeai, Workspace operator and FileServer operator Helm charts at their pinned versions, install the sshpiper Pipe CRD and the SSH operator from its Helm chart (see [SSH](#ssh)), and see each component's status |

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

`deploy/dev` combines `config/default` with the platform ConfigMap. It does not include the Project CRD or controller: install the project chart for Projects (see below). `make render-dev` only renders `dist/dev.yaml`; `make undeploy-dev` removes everything, including the CRDs.

### Helm charts

The forgeai operator ships as a Helm chart in `charts/chart`: the InferenceService CRD, RBAC and the controller Deployment, with the platform config under `platform.config`. The workspace operator (`workspace.wutz.dev`, see [Projects](#projects) and [InstanceType](#instancetype)) and the FileServer operator are separate projects with their own charts. The Dependencies view installs three charts into `forgeai-system` at their pinned versions, in this order. The same installs from the CLI:

```sh
helm install workspace oci://ghcr.io/wutz/charts/workspace --version 0.5.2 \
  --namespace forgeai-system --create-namespace
helm install forgeai oci://ghcr.io/wutz/charts/forgeai --version 0.5.0 \
  --namespace forgeai-system
helm install fileserver oci://ghcr.io/wutz/charts/fileserver --version 0.3.0 \
  --namespace forgeai-system
```

forgeai reads the InstanceTypes that the workspace operator creates. The workspace operator is [wutz/workspace](https://github.com/wutz/workspace). It runs in the platform namespace and reads the `workspace-platform` ConfigMap from its own namespace (`PROJECT_NAMESPACE`), which the forgeai chart renders. The FileServer operator is [wutz/fileserver](https://github.com/wutz/fileserver), see [FileServer](#fileserver).

Earlier releases served Projects and InstanceTypes from the `forgeai.wutz.dev` group (`projects.forgeai.wutz.dev`, `instancetypes.forgeai.wutz.dev`), and the InstanceType operator was a separate chart. The workspace chart serves them from `workspace.wutz.dev`, so the new CRDs do not clash with the old ones and nothing moves over by itself. The old CRDs carry `helm.sh/resource-policy: keep`, so they and their objects stay until removed. To move a cluster, uninstall the `forgeai-instancetype-operator` release, relabel each project namespace from `forgeai.wutz.dev/project` to `workspace.wutz.dev/project` (the GUI lists only namespaces with the new label), recreate each Project as a `workspace.wutz.dev/v1alpha1` object, then delete the `forgeai.wutz.dev` CRDs. This path is not verified on a live cluster.

Pushing a `v<version>` tag runs `.github/workflows/helm-release.yml`, which publishes the operator image (`ghcr.io/wutz/forgeai:<version>`) and the chart (`oci://ghcr.io/wutz/charts/forgeai:<version>`). `make helm-lint` lints and renders the chart; `make helm-package CHART_VERSION=<version>` writes the package to `dist/`.

The SSH gateway operator is a separate project, [wutz/sshgateway](https://github.com/wutz/sshgateway), published as the chart `oci://ghcr.io/wutz/charts/sshgateway`. The Dependencies view installs the sshpiper `Pipe` CRD, then the pinned chart version (0.3.0) as release `sshgateway` in `sshgateway-system`, with `gateway.namespace` set to the platform namespace. The same install from the CLI, once the Pipe CRD is in the cluster:

```sh
helm install sshgateway oci://ghcr.io/wutz/charts/sshgateway --version 0.3.0 \
  --namespace sshgateway-system --create-namespace \
  --set gateway.namespace=forgeai-system
```

A cluster that ran the SSH gateway from an earlier forgeai release has the `SSHGatewayClass`, `SSHGateway` and `SSHRoute` CRDs of the `forgeai.wutz.dev` group, installed by the `ssh-operator` release. The new chart serves `sshgateway.wutz.dev` and does not remove them. Back up the objects first (`kubectl get sshgatewayclasses,sshgateways,sshroutes -A -o yaml > ssh-backup.yaml`), uninstall the `ssh-operator` release, delete the three `forgeai.wutz.dev` CRDs, install the chart, and recreate the objects as `sshgateway.wutz.dev/v1alpha1`. Gateway Deployments are selected by a label that changed, so recreate them too. This path is not verified on a cluster that ran the earlier release.

A cluster set up with `make deploy-dev` runs the operator outside Helm. Helm does not adopt objects it did not create, so the Dependencies view reports that operator as deployed outside Helm and refuses the chart install. Move such a cluster by removing the dev deployment with `make undeploy-dev` (this deletes the forgeai CRDs and all forgeai objects, so back them up first) and then installing the charts.

Prerequisites (the Dependencies view installs Kubeflow Trainer, agent-sandbox, Kueue, the project, forgeai and FileServer operators, the sshpiper Pipe CRD and the SSH gateway into a cluster; `make gui-deps` refreshes the pinned manifests embedded in the GUI):

- Kubeflow Trainer v2 (with JobSet) and a `ClusterTrainingRuntime` named after `training.defaultRuntime`
- agent-sandbox v1.0 or later (CRDs `agents.x-k8s.io/v1beta1`), for the Sandboxes view; the isolation RuntimeClasses (`gvisor`, `kata`) come from the node runtimes
- Kueue (optional), with the `trainer.kubeflow.org/trainjob` integration enabled (the bundled Kueue config enables it). Without Kueue there is no queue, and stopping a TrainJob sets its `spec.suspend` flag instead
- Workspace operator (`workspace` chart), which creates the InstanceTypes and, when Kueue is installed, its ResourceFlavors from GPU nodes
- NVIDIA GPU operator (GPU feature discovery labels and the `nvidia.com/gpu` resource); not installed from the GUI
- A CNI that enforces NetworkPolicies, an ingress controller and Harbor
- For SSH sandboxes only: the sshpiper `Pipe` CRD and the [SSH gateway operator](https://github.com/wutz/sshgateway), see [SSH](#ssh)
