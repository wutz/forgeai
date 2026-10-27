package app

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// operatorModule is the InstanceType operator, which owns the InstanceType CRD.
const operatorModule = "github.com/wutz/forgeai-instancetype-operator"

// crdDirs returns the CRD directories for envtest: forgeai's own CRDs and the
// ones shipped by the operator module this build depends on.
func crdDirs(t *testing.T) []string {
	t.Helper()
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", operatorModule).Output()
	if err != nil {
		t.Fatalf("locate %s: %v", operatorModule, err)
	}
	return []string{
		"../../config/crd/bases",
		filepath.Join(strings.TrimSpace(string(out)), "config", "crd", "bases"),
	}
}
