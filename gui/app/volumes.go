package app

import (
	"context"
	"time"

	"github.com/wutz/forgeai/pkg/platform"
	"github.com/wutz/forgeai/pkg/volumes"
)

// Volume is the view model of a project volume (PVC).
type Volume = volumes.Volume

// VolumesClient wraps the volumes package for one connection.
func volumesClient(c *Connection) *volumes.Client {
	return &volumes.Client{K8s: c.k8s, Namespace: c.namespace()}
}

// ListVolumes lists live volumes; trash lists the recycle bin instead.
func (a *App) ListVolumes(trash bool) ([]Volume, error) {
	c, err := a.current()
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.call()
	defer cancel()
	return volumesClient(c).List(ctx, trash)
}

// CreateVolume creates a volume of a storage type (shared, ...), size in GiB.
func (a *App) CreateVolume(name, volType string, sizeGiB int64) (*Volume, error) {
	c, err := a.current()
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.call()
	defer cancel()
	cfg, err := platform.Load(ctx, c.k8s, c.namespace())
	if err != nil {
		return nil, err
	}
	return volumesClient(c).Create(ctx, cfg, name, volType, sizeGiB)
}

// ResizeVolume grows a volume (shrinking is not possible in Kubernetes). The
// change takes effect once the storage class controller has caught up.
func (a *App) ResizeVolume(name string, sizeGiB int64) (*Volume, error) {
	c, err := a.current()
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.call()
	defer cancel()
	return volumesClient(c).Resize(ctx, name, sizeGiB)
}

// TrashVolume moves a volume to the recycle bin. It stays restorable until
// the retention expires; mounted volumes are refused.
func (a *App) TrashVolume(name string) (*Volume, error) {
	c, err := a.current()
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.call()
	defer cancel()
	cfg, err := platform.Load(ctx, c.k8s, c.namespace())
	if err != nil {
		return nil, err
	}
	retention, err := trashRetention(ctx, c, cfg)
	if err != nil {
		return nil, err
	}
	return volumesClient(c).Trash(ctx, name, retention)
}

// trashRetention is how long a volume trashed now is kept: its project's
// retention when it sets one, else the platform default.
func trashRetention(ctx context.Context, c *Connection, cfg *platform.Config) (time.Duration, error) {
	retention, err := volumes.Retention(ctx, c.k8s, c.namespace(), cfg.TrashRetention.Duration)
	if err != nil {
		return 0, err
	}
	if retention <= 0 {
		retention = 7 * 24 * time.Hour
	}
	return retention, nil
}

// SetVolumeExpiry overrides when a trashed volume is purged.
func (a *App) SetVolumeExpiry(name string, at time.Time) (*Volume, error) {
	c, err := a.current()
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.call()
	defer cancel()
	return volumesClient(c).SetExpiry(ctx, name, at)
}

// ResetVolumeExpiry puts a trashed volume's purge time back to the default for
// its project, undoing SetVolumeExpiry.
func (a *App) ResetVolumeExpiry(name string) (*Volume, error) {
	c, err := a.current()
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.call()
	defer cancel()
	cfg, err := platform.Load(ctx, c.k8s, c.namespace())
	if err != nil {
		return nil, err
	}
	retention, err := trashRetention(ctx, c, cfg)
	if err != nil {
		return nil, err
	}
	return volumesClient(c).ResetExpiry(ctx, name, retention)
}

// RestoreVolume takes a volume out of the recycle bin.
func (a *App) RestoreVolume(name string) (*Volume, error) {
	c, err := a.current()
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.call()
	defer cancel()
	return volumesClient(c).Restore(ctx, name)
}

// PurgeVolume deletes a trashed volume immediately.
func (a *App) PurgeVolume(name string) error {
	c, err := a.current()
	if err != nil {
		return err
	}
	ctx, cancel := a.call()
	defer cancel()
	return volumesClient(c).Purge(ctx, name)
}
