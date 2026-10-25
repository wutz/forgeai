// Command template publishes a forgeai template source to an OCI registry, so
// that others can read it with an oci:// template source.
//
//	go run ./cmd/template publish ./my-templates oci://registry.example.com/team/templates:v1
//
// The source is checked before anything is pushed. The command prints the
// reference pinned by digest: share that one, since a tag can be moved. Logins
// come from the Docker configuration (docker login).
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wutz/forgeai/pkg/catalog"
)

func main() {
	if len(os.Args) != 4 || os.Args[1] != "publish" {
		fmt.Fprintln(os.Stderr, "usage: template publish <dir> oci://registry/repository:tag")
		os.Exit(2)
	}
	dir, err := filepath.Abs(os.Args[2])
	if err != nil {
		fmt.Fprintln(os.Stderr, "template:", err)
		os.Exit(1)
	}
	pinned, err := catalog.Publish(context.Background(), dir, os.Args[3])
	if err != nil {
		fmt.Fprintln(os.Stderr, "template:", err)
		os.Exit(1)
	}
	fmt.Println(pinned)
}
