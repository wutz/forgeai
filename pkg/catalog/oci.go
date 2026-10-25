package catalog

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/errdef"
	"oras.land/oras-go/v2/registry"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/credentials"
	"oras.land/oras-go/v2/registry/remote/retry"
)

// A template source can be published to an OCI registry as an artifact, so
// it can be pushed by one person and pulled by another. The artifact has one
// layer: a gzipped tar of the source's index.yaml and the template files it
// lists. oras pulls it as well (oras pull oci://... works on the same tag).
const (
	// ArtifactType is the artifactType of a published template source.
	ArtifactType = "application/vnd.forgeai.template.source.v1"
	// LayerMediaType is the media type of the source layer: a gzipped tar.
	LayerMediaType = "application/vnd.forgeai.template.source.layer.v1.tar+gzip"
	// maxArchiveBytes bounds an unpacked source; templates are a few kilobytes.
	maxArchiveBytes = 16 << 20
	// maxArchiveFiles bounds the entries of one source.
	maxArchiveFiles = 1000
	// maxManifestBytes bounds the manifest of a source.
	maxManifestBytes = 1 << 20
)

// ociScheme is the prefix of an OCI template source location.
const ociScheme = "oci://"

// parseOCI parses oci://registry/repository:tag or oci://registry/repository@sha256:...
// A location without a tag or digest is refused: the source must name what it reads.
func parseOCI(location string) (registry.Reference, error) {
	ref, err := registry.ParseReference(strings.TrimPrefix(location, ociScheme))
	if err != nil || ref.Registry == "" || ref.Reference == "" {
		return registry.Reference{}, fmt.Errorf("template source %q must be oci://registry/repository:tag or oci://registry/repository@sha256:digest", location)
	}
	return ref, nil
}

// newRepository returns the registry repository of ref. Credentials come from
// the Docker configuration, so the same login works for pulls and pushes.
func newRepository(ref registry.Reference, client *http.Client, plainHTTP bool) (*remote.Repository, error) {
	repo, err := remote.NewRepository(ref.Registry + "/" + ref.Repository)
	if err != nil {
		return nil, fmt.Errorf("template source %s: %w", ref, err)
	}
	store, err := credentials.NewStoreFromDocker(credentials.StoreOptions{})
	if err != nil {
		return nil, fmt.Errorf("template source %s: read registry credentials: %w", ref, err)
	}
	if client == nil {
		client = retry.DefaultClient
	}
	repo.PlainHTTP = plainHTTP
	repo.Client = &auth.Client{Client: client, Cache: auth.NewCache(), Credential: store.Get}
	return repo, nil
}

// ociSource reads a published template source. The artifact is fetched once,
// on the first read, and its files are kept in memory.
type ociSource struct {
	ref  registry.Reference
	repo *remote.Repository

	mu    sync.Mutex
	files map[string][]byte
}

func newOCISource(location string, client *http.Client, plainHTTP bool) (*ociSource, error) {
	ref, err := parseOCI(location)
	if err != nil {
		return nil, err
	}
	repo, err := newRepository(ref, client, plainHTTP)
	if err != nil {
		return nil, err
	}
	return &ociSource{ref: ref, repo: repo}, nil
}

func (o *ociSource) Read(ctx context.Context, rel string) ([]byte, error) {
	files, err := o.load(ctx)
	if err != nil {
		return nil, err
	}
	data, ok := files[rel]
	if !ok {
		return nil, fmt.Errorf("%s: %w", rel, errNotInSource)
	}
	return data, nil
}

// errNotInSource is returned for a file the published source does not contain.
var errNotInSource = errors.New("not in the published source")

func (o *ociSource) load(ctx context.Context) (map[string][]byte, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.files != nil {
		return o.files, nil
	}
	fail := func(err error) error { return fmt.Errorf("template source %s: %w", o.ref, err) }

	desc, err := o.repo.Resolve(ctx, o.ref.Reference)
	if err != nil {
		return nil, fail(err)
	}
	if desc.Size > maxManifestBytes {
		return nil, fail(fmt.Errorf("manifest is %d bytes, more than the %d allowed", desc.Size, maxManifestBytes))
	}
	raw, err := fetchBlob(ctx, o.repo, desc)
	if err != nil {
		return nil, fail(err)
	}
	var m ocispec.Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fail(fmt.Errorf("parse manifest: %w", err))
	}
	if m.ArtifactType != ArtifactType || len(m.Layers) != 1 {
		return nil, fail(fmt.Errorf("not a template source: want artifact type %s with one layer", ArtifactType))
	}
	layer := m.Layers[0]
	if layer.MediaType != LayerMediaType {
		return nil, fail(fmt.Errorf("layer media type is %s, want %s", layer.MediaType, LayerMediaType))
	}
	if layer.Size > maxArchiveBytes {
		return nil, fail(fmt.Errorf("layer is %d bytes, more than the %d allowed", layer.Size, maxArchiveBytes))
	}
	data, err := fetchBlob(ctx, o.repo, layer)
	if err != nil {
		return nil, fail(err)
	}
	files, err := unpackSource(data)
	if err != nil {
		return nil, fail(err)
	}
	o.files = files
	return files, nil
}

// fetchBlob reads a blob and checks it against its digest.
func fetchBlob(ctx context.Context, repo *remote.Repository, desc ocispec.Descriptor) ([]byte, error) {
	rc, err := repo.Fetch(ctx, desc)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return content.ReadAll(rc, desc)
}

// unpackSource reads the gzipped tar of a source. Only regular files with
// clean relative names are accepted; the total size and the file count are bounded.
func unpackSource(data []byte) (map[string][]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("unpack source: %w", err)
	}
	defer gz.Close()
	budget := &io.LimitedReader{R: gz, N: maxArchiveBytes + 1}
	tr := tar.NewReader(budget)
	files := map[string][]byte{}
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("unpack source: %w", err)
		}
		if hdr.Typeflag == tar.TypeDir {
			continue
		}
		if hdr.Typeflag != tar.TypeReg {
			return nil, fmt.Errorf("unpack source: %s is not a regular file", hdr.Name)
		}
		name, err := cleanArchiveName(hdr.Name)
		if err != nil {
			return nil, err
		}
		if _, dup := files[name]; dup {
			return nil, fmt.Errorf("unpack source: %s is listed twice", name)
		}
		if len(files) >= maxArchiveFiles {
			return nil, fmt.Errorf("unpack source: more than %d files", maxArchiveFiles)
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("unpack source: %w", err)
		}
		files[name] = body
	}
	if budget.N <= 0 {
		return nil, fmt.Errorf("unpack source: more than %d bytes unpacked", maxArchiveBytes)
	}
	return files, nil
}

// cleanArchiveName returns the name of a file in a source, relative to its root.
func cleanArchiveName(name string) (string, error) {
	clean := strings.TrimPrefix(name, "./")
	if clean == "" || path.IsAbs(clean) || path.Clean(clean) != clean || clean == "." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("unpack source: %q is not a clean relative path", name)
	}
	return clean, nil
}

// Publish packs the template source in dir and pushes it to the OCI reference
// location (oci://registry/repository:tag). It returns the reference pinned by
// digest, which is the form to share: a tag can be moved, a digest cannot.
// Only a source that loads without template errors is published.
func Publish(ctx context.Context, dir, location string) (string, error) {
	return publish(ctx, dir, location, nil, false)
}

func publish(ctx context.Context, dir, location string, client *http.Client, plainHTTP bool) (string, error) {
	ref, err := parseOCI(location)
	if err != nil {
		return "", err
	}
	archive, err := packSource(ctx, dir)
	if err != nil {
		return "", err
	}
	repo, err := newRepository(ref, client, plainHTTP)
	if err != nil {
		return "", err
	}
	desc, err := pushSource(ctx, repo, ref.Reference, archive)
	if err != nil {
		return "", fmt.Errorf("template source %s: %w", ref, err)
	}
	return ociScheme + ref.Registry + "/" + ref.Repository + "@" + desc.Digest.String(), nil
}

// pushSource pushes the layer and the manifest of a source, and tags the
// manifest with reference when it is a tag. A digest reference must be the
// digest of the manifest.
func pushSource(ctx context.Context, repo *remote.Repository, reference string, archive []byte) (ocispec.Descriptor, error) {
	layer := content.NewDescriptorFromBytes(LayerMediaType, archive)
	exists, err := repo.Exists(ctx, layer)
	if err != nil {
		return ocispec.Descriptor{}, err
	}
	if !exists {
		if err := repo.Push(ctx, layer, bytes.NewReader(archive)); err != nil {
			return ocispec.Descriptor{}, err
		}
	}
	desc, err := oras.PackManifest(ctx, repo, oras.PackManifestVersion1_1, ArtifactType, oras.PackManifestOptions{
		Layers: []ocispec.Descriptor{layer},
	})
	if err != nil && !errors.Is(err, errdef.ErrAlreadyExists) {
		return ocispec.Descriptor{}, err
	}
	if strings.HasPrefix(reference, "sha256:") {
		if reference != desc.Digest.String() {
			return ocispec.Descriptor{}, fmt.Errorf("digest %s does not match the pushed manifest %s", reference, desc.Digest)
		}
		return desc, nil
	}
	if err := repo.Tag(ctx, desc, reference); err != nil {
		return ocispec.Descriptor{}, err
	}
	return desc, nil
}

// packSource returns the gzipped tar of the source in dir: index.yaml and the
// template files it lists, in name order, so the same source always packs to
// the same bytes.
func packSource(ctx context.Context, dir string) ([]byte, error) {
	c, err := Load(ctx, dirSource(dir))
	if err != nil {
		return nil, err
	}
	names := []string{IndexPath}
	for _, t := range c.Templates {
		if t.Error != "" {
			return nil, fmt.Errorf("template %s: %s", t.Name, t.Error)
		}
		names = append(names, t.Path)
	}
	slices.Sort(names)
	var buf bytes.Buffer
	gz, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	tw := tar.NewWriter(gz)
	// The epoch keeps the archive reproducible; a zero time is not a valid tar timestamp.
	epoch := time.Unix(0, 0)
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil {
			return nil, err
		}
		hdr := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg, ModTime: epoch}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, err
		}
		if _, err := tw.Write(data); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
