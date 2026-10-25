#!/usr/bin/env bash
# Exports the agent-sandbox CRDs (core and extensions) and the Gateway API
# HTTPRoute CRD into the controller test data, so envtest can run the Sandbox
# controller without agent-sandbox or a Gateway API installed. Refresh after
# upgrading agent-sandbox on the dev cluster:
#
#   hack/export-sandbox-crds.sh
#
# The upstream release bundle (sandbox-with-extensions.yaml) works too:
#
#   curl -sL https://github.com/kubernetes-sigs/agent-sandbox/releases/latest/download/sandbox.yaml |
#     yq 'select(.kind == "CustomResourceDefinition")' -o json | ...
set -euo pipefail

dir="$(cd "$(dirname "$0")/.." && pwd)/internal/controller/testdata/crds"
mkdir -p "$dir"

for crd in sandboxes.agents.x-k8s.io \
           sandboxtemplates.extensions.agents.x-k8s.io \
           sandboxclaims.extensions.agents.x-k8s.io \
           sandboxwarmpools.extensions.agents.x-k8s.io \
           httproutes.gateway.networking.k8s.io; do
  # Keep what envtest installs: the spec and the approval annotation that the
  # apiserver requires of protected groups. Cluster state (status, UID,
  # resourceVersion) is dropped, as the apiserver rejects it on create.
  kubectl get crd "$crd" -o json | jq '{
    apiVersion, kind,
    metadata: {name: .metadata.name,
               annotations: ((.metadata.annotations // {}) | with_entries(select(.key == "api-approved.kubernetes.io")))},
    spec
  }' > "$dir/$crd.json"
  echo "exported $crd"
done
