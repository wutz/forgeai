package app

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// operatorModules are the operator modules this build depends on. Each owns
// the CRDs of its operator, which envtest must load next to forgeai's own.
var operatorModules = []string{
	"github.com/wutz/forgeai-instancetype-operator", // InstanceType
	"github.com/wutz/fileserver-operator",           // FileServer
}

// crdDirs returns the CRD directories for envtest: forgeai's own CRDs and the
// ones shipped by the operator modules.
func crdDirs(t *testing.T) []string {
	t.Helper()
	dirs := []string{"../../config/crd/bases"}
	for _, module := range operatorModules {
		out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", module).Output()
		if err != nil {
			t.Fatalf("locate %s: %v", module, err)
		}
		dirs = append(dirs, filepath.Join(strings.TrimSpace(string(out)), "config", "crd", "bases"))
	}
	return dirs
}
