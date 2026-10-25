#!/usr/bin/env bash
# Refreshes the cluster dependency manifests embedded in the GUI (gui/app/deps).
#
# Kueue, agent-sandbox and the Gateway API Inference Extension publish their
# install manifests as release assets;
# each is checked against the sha256 pinned here before it is copied in.
# Kubeflow Trainer has no such asset, so its manifests (Trainer with its JobSet,
# and the built-in ClusterTrainingRuntimes) are rendered from the tag with
# kustomize. deps/manifest.json records the version and sha256 of every bundle;
# the GUI reads the versions from it and its tests verify the digests.
#
# After bumping a version here, run the script and commit the result:
#
#   KUSTOMIZE=bin/kustomize hack/gui-deps.sh
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
dir="$root/gui/app/deps"
kustomize="${KUSTOMIZE:-kustomize}"

TRAINER_VERSION=v2.3.0
KUEUE_VERSION=v0.20.2
KUEUE_SHA256=1ab14ecd590575ac57f50fa4cbcf529766bc2c32ba974bda1614381281ec52b4
SANDBOX_VERSION=v1.0.7
SANDBOX_SHA256=136c10c9347cb5a6740b351444cae82f404e883c3f520272257ba785f2af0da8
GAIE_VERSION=v1.6.2
GAIE_SHA256=f410dfca474608c02903087b47f1ac081706dfb6dfe7f83d00a7cc7e00c4ad0d

# fetch URL SHA256 FILE downloads a release asset and checks its digest.
fetch() {
  curl -fsSL --retry 3 -o "$3" "$1"
  echo "$2  $3" | shasum -a 256 -c - >/dev/null || { echo "digest mismatch for $1" >&2; exit 1; }
}

# sha prints the sha256 of a file.
sha() { shasum -a 256 "$1" | cut -d' ' -f1; }

mkdir -p "$dir"

# Trainer first: Kueue's TrainJob integration needs its CRDs.
trainer="github.com/kubeflow/trainer//manifests/overlays"
{
  "$kustomize" build "$trainer/manager?ref=$TRAINER_VERSION"
  echo ---
  "$kustomize" build "$trainer/runtimes?ref=$TRAINER_VERSION"
} > "$dir/trainer.yaml"

fetch "https://github.com/kubernetes-sigs/kueue/releases/download/$KUEUE_VERSION/manifests.yaml" \
  "$KUEUE_SHA256" "$dir/kueue.yaml"
fetch "https://github.com/kubernetes-sigs/agent-sandbox/releases/download/$SANDBOX_VERSION/sandbox-with-extensions.yaml" \
  "$SANDBOX_SHA256" "$dir/agent-sandbox.yaml"
# The InferencePool CRD the llm-d routers of InferenceServices need.
fetch "https://github.com/kubernetes-sigs/gateway-api-inference-extension/releases/download/$GAIE_VERSION/v1-manifests.yaml" \
  "$GAIE_SHA256" "$dir/gateway-api-inference-extension.yaml"

# The order is the install order.
cat > "$dir/manifest.json" <<EOF
{
  "bundles": [
    {
      "name": "trainer",
      "version": "$TRAINER_VERSION",
      "source": "https://github.com/kubeflow/trainer/tree/$TRAINER_VERSION/manifests",
      "file": "trainer.yaml",
      "sha256": "$(sha "$dir/trainer.yaml")"
    },
    {
      "name": "kueue",
      "version": "$KUEUE_VERSION",
      "source": "https://github.com/kubernetes-sigs/kueue/releases/tag/$KUEUE_VERSION",
      "file": "kueue.yaml",
      "sha256": "$(sha "$dir/kueue.yaml")"
    },
    {
      "name": "agent-sandbox",
      "version": "$SANDBOX_VERSION",
      "source": "https://github.com/kubernetes-sigs/agent-sandbox/releases/tag/$SANDBOX_VERSION",
      "file": "agent-sandbox.yaml",
      "sha256": "$(sha "$dir/agent-sandbox.yaml")"
    },
    {
      "name": "gateway-api-inference-extension",
      "version": "$GAIE_VERSION",
      "source": "https://github.com/kubernetes-sigs/gateway-api-inference-extension/releases/tag/$GAIE_VERSION",
      "file": "gateway-api-inference-extension.yaml",
      "sha256": "$(sha "$dir/gateway-api-inference-extension.yaml")"
    }
  ]
}
EOF
echo "wrote $dir"
