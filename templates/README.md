# Public template source

This directory is the public template source of forgeai. The GUI lists its job
and sandbox templates in the New job and New sandbox dialogs and prefills the
form from the one you pick. A template never touches a cluster by itself: you
review the prefilled form and submit it, as you would any job or sandbox.

The design follows [kubeyard-catalog](https://github.com/wutz/kubeyard-catalog):
a small index that summarizes the catalog, one file per item, and a validator
that runs before anything is published.

## Layout

```
templates/
  index.yaml                          the index: every template and its summary
  jobs/<name>/template.yaml           a job template (kind Job)
  sandboxes/<name>/template.yaml      a sandbox template (kind Sandbox)
```

Only `index.yaml` and the files it lists are read.

## Index

```yaml
apiVersion: templates.forgeai.wutz.dev/v1alpha1
kind: TemplateIndex
metadata:
  name: forgeai-default
spec:
  displayName: forgeai default templates
  homepage: https://github.com/wutz/forgeai
  templates:
    - name: nccl-tests
      kind: Job                      # Job or Sandbox
      displayName: NCCL tests
      description: All-reduce bandwidth of every GPU on each worker.
      version: 1.1.0
      path: jobs/nccl-tests/template.yaml
```

Rules, checked when the source is loaded:

- `apiVersion` and `kind` are exactly `templates.forgeai.wutz.dev/v1alpha1` and `TemplateIndex`.
- At least one template is listed. Unknown fields are an error.
- `kind` is `Job` or `Sandbox`. `name` is unique per kind.
- `path` is a clean relative path inside the source: no leading `/`, no `..`, no `//`.

An index that cannot be read or parsed fails the whole source; the GUI then
shows the error in the dialog and offers no catalog templates.

## Template files

A job template:

```yaml
apiVersion: templates.forgeai.wutz.dev/v1alpha1
kind: JobTemplate
metadata:
  name: nccl-tests                   # must equal the index name
spec:
  displayName: NCCL tests            # must equal the index entry
  description: All-reduce bandwidth of every GPU on each worker.
  version: 1.1.0                     # must equal the index entry
  image: ghcr.io/coreweave/nccl-tests:12.9.1-devel-ubuntu22.04-nccl2.27.6-1-7c12c62
  command: |
    set -euo pipefail
    exec /opt/nccl_tests/build/all_reduce_perf -b 8 -e 8G -f 2 -g "$(nvidia-smi -L | wc -l)"
  workers: 1
  maxRetries: 0                      # optional
  retryOn: Infra                     # optional: Infra or Any
  env:
    - name: NCCL_ALGO
      value: Ring
    - name: NCCL_DEBUG
      value: WARN
```

A sandbox template:

```yaml
apiVersion: templates.forgeai.wutz.dev/v1alpha1
kind: SandboxTemplate
metadata:
  name: python-runtime
spec:
  displayName: Python runtime
  description: Python agent sandbox from agent-sandbox.
  version: 1.0.0
  image: registry.k8s.io/agent-sandbox/python-runtime-sandbox:v0.1.0
  command: ""                        # optional
  runtimeClass: ""                   # optional: a RuntimeClass the platform offers
  env: []                            # optional
```

Both kinds decode strictly: an unknown field is an error, so a typo does not
silently drop a setting. The file's `metadata.name` must equal the index name,
and its `displayName`, `description` and `version` must equal the index entry,
so the list the GUI shows is the list the files say.

A template is a prefill, so it does not choose the job name, the instance type
or the sandbox name, and it does not choose volumes. Those stay with the user.

A template with a broken file is still listed, with its error, so one bad file
does not hide the others. The GUI does not offer a template that has an error.

## Validation

`pkg/catalog` loads a source and runs the rules above, and it validates each
template with the same checks the GUI applies to a form it submits:

- Job templates: `training.Form.Validate` with a placeholder instance type.
- Sandbox templates: `sandbox.RenderTemplate`, so an image is required and the
  command, env and runtime class must render.

```bash
GOWORK=off go test ./pkg/catalog/
```

`TestRepoTemplates` loads this directory and fails when a template file exists
without an index entry, or when a template does not load.

## Where the GUI reads it

The platform config names the source:

```yaml
templates:
  source: https://raw.githubusercontent.com/wutz/forgeai/main/templates
```

- `https://` (or `http://`) is a base URL; the GUI fetches `index.yaml` and the
  listed files from it.
- `file://` is an absolute directory, for a local checkout.
- Empty uses the forgeai default: `https://raw.githubusercontent.com/wutz/forgeai/main/templates`.

The GUI reads the source each time a create dialog opens, so a fixed template
shows up without restarting the app.

## Images

A job or sandbox template names an image, and the image must be pullable from
the cluster. The templates use public images and forgeai builds none:

- `nccl-tests` runs the public CoreWeave image
  `ghcr.io/coreweave/nccl-tests`, the one the scclabs `k8s-in-action`
  `ai/nccl-tests` guide uses. Its CUDA 12.9 tag runs on RTX 5090 nodes; use the
  CUDA 13.1 tag (`13.1.0-devel-ubuntu22.04-nccl2.29.2-1-2276a5e`) on B200 and
  B300. The binaries are in `/opt/nccl_tests/build`.
- `nccl-tests` is single node: forgeai workers expose no SSH, so the MPI
  launcher of the reference guide does not apply. Each worker runs
  `all_reduce_perf` over its own GPUs, so a pool is checked with one worker per
  node. The pass criteria of the guide carry over: no NCCL error, 0 wrong
  values in every row, and a peak `busbw` near the hardware limit.

A cluster that cannot reach `ghcr.io` mirrors the image into its project
registry and overrides the image in the form.

## Adding a template

1. Add the template file under `jobs/<name>/` or `sandboxes/<name>/`.
2. Add its entry to `index.yaml`, with the same `displayName`, `description` and `version`.
3. Run `GOWORK=off go test ./pkg/catalog/`.
4. Bump `version` when the template changes in a way a user would notice.

## Not yet

- Signing: the source is not signed. Signing the index with cosign, and
  publishing it as an OCI artifact that Flux can reconcile, is a follow-up.
  Until then the source is trusted by its URL, so `templates.source` should
  point to a location the platform team controls.
