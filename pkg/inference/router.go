// Package inference builds the objects of an InferenceService: the vLLM model
// servers, the llm-d router that balances requests across them, and the
// Ingress of the endpoint.
package inference

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/engine"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"

	"github.com/wutz/forgeai/pkg/naming"
)

// routerChart is the llm-d router chart v1.0.0 in standalone mode, pulled from
// oci://ghcr.io/llm-d/charts/llm-d-router-standalone. Its Endpoint Picker image
// is the platform's router image at the same version.
//
//go:embed charts/llm-d-router-standalone-v1.0.0.tgz
var routerChart []byte

// Router renders the llm-d router of the InferenceService name: the Endpoint
// Picker (EPP) with its Envoy proxy, the InferencePool selecting the model
// servers labelled with the name, and the Services, ServiceAccount and RBAC of
// the router. The EPP runs replicas pods, 0 or 1.
func Router(name, namespace, image string, replicas int32) ([]*unstructured.Unstructured, error) {
	c, err := loader.LoadArchive(bytes.NewReader(routerChart))
	if err != nil {
		return nil, fmt.Errorf("load llm-d router chart: %w", err)
	}
	registry, repository, tag := splitImage(image)
	values := map[string]any{
		"router": map[string]any{
			"modelServers": map[string]any{
				"matchLabels": map[string]any{naming.LabelInference: name},
				"targetPorts": []any{map[string]any{"number": ModelPort}},
				"protocol":    "http",
			},
			"extraServicePorts": []any{map[string]any{
				"name": portName, "port": 80, "protocol": "TCP", "targetPort": routerPort,
			}},
			// The chart treats 0 as unset, so the EPP replicas are set on the
			// rendered Deployment below.
			"epp": map[string]any{
				"replicas":          1,
				"pluginsConfigFile": "default-plugins.yaml",
				"image": map[string]any{
					"registry": registry, "repository": repository, "tag": tag, "pullPolicy": "IfNotPresent",
				},
			},
			// Each router has its own Envoy ConfigMap; the chart's default name
			// would collide between InferenceServices.
			"proxy": map[string]any{"presets": map[string]any{"envoy": map[string]any{
				"configMap": map[string]any{"name": name + "-envoy"},
			}}},
			// The router's metrics are not scraped and its ClusterRole is not
			// created: forgeai runs one router per namespace.
			"monitoring": map[string]any{"prometheus": map[string]any{"auth": map[string]any{"enabled": false}}},
		},
	}
	vals, err := chartutil.ToRenderValues(c, values, chartutil.ReleaseOptions{
		Name: name, Namespace: namespace, IsInstall: true,
	}, chartutil.DefaultCapabilities)
	if err != nil {
		return nil, err
	}
	files, err := engine.Render(c, vals)
	if err != nil {
		return nil, fmt.Errorf("render llm-d router chart: %w", err)
	}
	var objs []*unstructured.Unstructured
	for _, file := range slices.Sorted(maps.Keys(files)) {
		// Only the manifests: NOTES.txt and the helpers are not objects.
		if !strings.HasSuffix(file, ".yaml") {
			continue
		}
		dec := yaml.NewYAMLOrJSONDecoder(strings.NewReader(files[file]), 4096)
		for {
			obj := map[string]any{}
			if err := dec.Decode(&obj); err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				return nil, fmt.Errorf("decode %s: %w", file, err)
			}
			if len(obj) == 0 {
				continue
			}
			u := &unstructured.Unstructured{Object: obj}
			if u.GetNamespace() == "" {
				u.SetNamespace(namespace)
			}
			// The manager caches Deployments and Services by the managed-by
			// label, so an unlabelled router object is invisible to its client:
			// the EPP would read as never ready and its watch would not fire.
			labels := u.GetLabels()
			if labels == nil {
				labels = map[string]string{}
			}
			labels[naming.LabelManagedBy] = naming.ManagedBy
			labels[naming.LabelComponent] = naming.ComponentInference
			labels[naming.LabelInference] = name
			u.SetLabels(labels)
			if u.GetKind() == "Deployment" && u.GetName() == EPPName(name) {
				if err := unstructured.SetNestedField(u.Object, int64(replicas), "spec", "replicas"); err != nil {
					return nil, err
				}
			}
			objs = append(objs, u)
		}
	}
	return objs, nil
}

// splitImage splits an image reference into the registry, repository and tag
// the chart takes, e.g. ghcr.io/llm-d/router:v1.0.0.
func splitImage(ref string) (registry, repository, tag string) {
	if i := strings.LastIndex(ref, ":"); i > strings.LastIndex(ref, "/") {
		ref, tag = ref[:i], ref[i+1:]
	}
	if i := strings.LastIndex(ref, "/"); i >= 0 {
		return ref[:i], ref[i+1:], tag
	}
	return "", ref, tag
}
