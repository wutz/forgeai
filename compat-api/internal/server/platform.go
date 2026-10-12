package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/scclabs/forgeai/pkg/capacity"
	"github.com/scclabs/forgeai/pkg/images"
	"github.com/scclabs/forgeai/pkg/naming"
	"github.com/scclabs/forgeai/pkg/pools"
	"github.com/scclabs/forgeai/pkg/volumes"
)

// scc-k8s-v2 route prefixes besides jobsPrefix.
const (
	podPrefix         = "/apis/pod/v1alpha2"
	podUsersPrefix    = podPrefix + "/users"
	poolPrefix        = "/apis/pool/v1alpha2"
	poolRequestPrefix = "/apis/node/v1alpha2/users"
)

func msgErr(status int, format string, args ...any) error {
	return &msgError{Status: status, Msg: fmt.Sprintf(format, args...)}
}

// --- images ---

type ImageRef struct {
	ID string `json:"id" doc:"Full image reference"`
}

type warmupInput struct{ Body []ImageRef }
type warmupOutput struct{ Body []ImageRef }

type userInput struct {
	UserID string `path:"userID"`
}

type ImageList struct {
	Images []images.Image `json:"images"`
}
type imagesOutput struct{ Body ImageList }

func (s *Server) warmup(ctx context.Context, in *warmupInput) (*warmupOutput, error) {
	cfg, err := s.Jobs.Config(ctx)
	if err != nil {
		return nil, s.internal(err)
	}
	refs := make([]string, 0, len(in.Body))
	for _, i := range in.Body {
		refs = append(refs, i.ID)
	}
	if err := s.Warmer.Warmup(ctx, cfg, refs); err != nil {
		return nil, s.internal(err)
	}
	return &warmupOutput{Body: in.Body}, nil
}

// listImages returns {"images": []} on any registry error, like scc-k8s-v2.
func (s *Server) listImages(ctx context.Context, in *userInput) (*imagesOutput, error) {
	out := &imagesOutput{Body: ImageList{Images: []images.Image{}}}
	cfg, err := s.Jobs.Config(ctx)
	if err != nil {
		s.Log.Error("list images", "error", err)
		return out, nil
	}
	r := images.NewRegistry(cfg.Registry, s.RegistryUser, s.RegistryPassword)
	defer r.HTTP.CloseIdleConnections()
	imgs, err := r.List(ctx, s.Jobs.Namespace, in.UserID)
	if err != nil {
		s.Log.Error("list images", "user", in.UserID, "error", err)
		return out, nil
	}
	out.Body.Images = imgs
	return out, nil
}

// --- volumes ---

type VolumeIn struct {
	SizeGB int64  `json:"sizeGB" minimum:"1"`
	Type   string `json:"type,omitempty" default:"high-perf"`
}

type createVolumeInput struct {
	UserID   string `path:"userID"`
	VolumeID string `query:"volumeID" required:"true"`
	Body     VolumeIn
}

type volumeRef struct {
	UserID   string `path:"userID"`
	VolumeID string `path:"volumeID"`
}

type listVolumesInput struct {
	UserID   string `path:"userID"`
	VolumeID string `query:"volumeID"`
}

type resizeVolumeInput struct {
	UserID   string `path:"userID"`
	VolumeID string `path:"volumeID"`
	Body     VolumeIn
}

type volumeOutput struct{ Body volumes.Volume }

type VolumeList struct {
	Volumes []volumes.Volume `json:"volumes"`
}
type volumesOutput struct{ Body VolumeList }

type StatusBody struct {
	Status string `json:"status"`
}
type statusOutput struct{ Body StatusBody }

type usageOutput struct{ Body volumes.UsageInfo }

func volumeNotFound(volumeID string) error {
	return msgErr(http.StatusNotFound, "Volume '%s' was not found", volumeID)
}

func (s *Server) createVolume(ctx context.Context, in *createVolumeInput) (*volumeOutput, error) {
	cfg, err := s.Jobs.Config(ctx)
	if err != nil {
		return nil, s.internal(err)
	}
	v, err := s.Volumes.Create(ctx, cfg, in.UserID, in.VolumeID, in.Body.Type, in.Body.SizeGB)
	if err != nil {
		return nil, msgErr(http.StatusConflict, "Failed to create volume: %v", err)
	}
	return &volumeOutput{Body: *v}, nil
}

func (s *Server) listVolumes(ctx context.Context, in *listVolumesInput) (*volumesOutput, error) {
	vs, err := s.Volumes.List(ctx, in.UserID, in.VolumeID)
	if err != nil {
		s.Log.Error("list volumes", "error", err)
		return nil, msgErr(http.StatusInternalServerError, "Failed to list volumes: %v", err)
	}
	return &volumesOutput{Body: VolumeList{Volumes: vs}}, nil
}

func (s *Server) getVolume(ctx context.Context, in *volumeRef) (*volumeOutput, error) {
	v, err := s.Volumes.Get(ctx, in.UserID, in.VolumeID)
	if errors.Is(err, volumes.ErrNotFound) {
		return nil, volumeNotFound(in.VolumeID)
	}
	if err != nil {
		return nil, msgErr(http.StatusInternalServerError, "Get volume '%s' failed: %v", in.VolumeID, err)
	}
	return &volumeOutput{Body: *v}, nil
}

func (s *Server) deleteVolume(ctx context.Context, in *volumeRef) (*statusOutput, error) {
	err := s.Volumes.Delete(ctx, in.UserID, in.VolumeID)
	if errors.Is(err, volumes.ErrInUse) {
		s.Log.Info("volume in use", "error", err)
		return nil, msgErr(http.StatusConflict, "Volume %s were refered by pods",
			naming.VolumeClaimName(s.Volumes.Namespace, in.UserID, in.VolumeID))
	}
	if err != nil {
		return nil, msgErr(http.StatusConflict, "Failed to delete volume: %v", err)
	}
	return &statusOutput{Body: StatusBody{Status: "success"}}, nil
}

func (s *Server) resizeVolume(ctx context.Context, in *resizeVolumeInput) (*volumeOutput, error) {
	v, err := s.Volumes.Resize(ctx, in.UserID, in.VolumeID, in.Body.SizeGB)
	if errors.Is(err, volumes.ErrNotFound) {
		return nil, volumeNotFound(in.VolumeID)
	}
	if err != nil {
		return nil, msgErr(http.StatusConflict, "Failed to resize volume: %v", err)
	}
	return &volumeOutput{Body: *v}, nil
}

func (s *Server) volumeUsage(ctx context.Context, in *volumeRef) (*usageOutput, error) {
	cfg, err := s.Jobs.Config(ctx)
	if err != nil {
		return nil, s.internal(err)
	}
	u := &volumes.UsageClient{BaseURL: cfg.VolumeUsage.BaseURL, Token: s.VolumeUsageToken}
	info, err := u.Usage(ctx, s.Volumes.Namespace, in.UserID, in.VolumeID)
	if err != nil {
		if !errors.Is(err, volumes.ErrNotFound) {
			s.Log.Error("volume usage", "volume", in.VolumeID, "error", err)
		}
		return nil, msgErr(http.StatusNotFound, "Volume '%s' usage information was not found", in.VolumeID)
	}
	return &usageOutput{Body: *info}, nil
}

// --- pools ---

type PoolList struct {
	Pools map[string]pools.Pool `json:"pools"`
}
type poolsOutput struct{ Body PoolList }

type poolInput struct {
	PoolID string `path:"pool_id"`
}

type PoolPods struct {
	PoolID string          `json:"pool_id"`
	Pods   []pools.PodInfo `json:"pods"`
}
type poolPodsOutput struct{ Body PoolPods }

func (s *Server) listPools(ctx context.Context, _ *struct{}) (*poolsOutput, error) {
	ps, err := s.Pools.List(ctx)
	if err != nil {
		return nil, msgErr(http.StatusInternalServerError, "Failed to list pools: %v", err)
	}
	return &poolsOutput{Body: PoolList{Pools: ps}}, nil
}

func (s *Server) poolPods(ctx context.Context, in *poolInput) (*poolPodsOutput, error) {
	pods, err := s.Pools.Pods(ctx, in.PoolID)
	if err != nil {
		return nil, msgErr(http.StatusInternalServerError, "Failed to list pods in pool '%s': %v", in.PoolID, err)
	}
	return &poolPodsOutput{Body: PoolPods{PoolID: in.PoolID, Pods: pods}}, nil
}

// --- pool requests ---

type PoolRequestIn struct {
	SrcPool    string `json:"src_pool,omitempty" doc:"Source pool, default 'default'"`
	DstPool    string `json:"dst_pool,omitempty" doc:"Destination pool, default 'default'"`
	ResourceID string `json:"resourceID"`
	NNodes     int32  `json:"nnodes,omitempty" default:"1" minimum:"1"`
}

type createPoolRequestInput struct {
	UserID    string `path:"userID"`
	RequestID string `query:"request_id" required:"true"`
	Body      PoolRequestIn
}

type poolRequestRef struct {
	UserID    string `path:"userID"`
	RequestID string `path:"request_id"`
}

type poolRequestOutput struct{ Body pools.Request }

// poolRequestGetOutput is a pools.Request, or {} when it does not exist.
type poolRequestGetOutput struct{ Body any }

type RequestMsg struct {
	RequestID string `json:"request_id"`
	Msg       string `json:"msg"`
}

type requestMsgOutput struct {
	Status int
	Body   RequestMsg
}

type deletePoolInput struct {
	UserID string `path:"userID"`
	PoolID string `path:"poolID"`
}

type MsgBody struct {
	Msg string `json:"msg"`
}

type msgOutput struct {
	Status int
	Body   MsgBody
}

func (s *Server) createPoolRequest(ctx context.Context, in *createPoolRequestInput) (*poolRequestOutput, error) {
	cfg, err := s.Jobs.Config(ctx)
	if err != nil {
		return nil, s.internal(err)
	}
	r, err := s.Pools.CreateRequest(ctx, cfg, pools.RequestSpec{
		UserID: in.UserID, RequestID: in.RequestID, SrcPool: in.Body.SrcPool, DstPool: in.Body.DstPool,
		ResourceID: in.Body.ResourceID, NNodes: in.Body.NNodes,
	})
	if errors.Is(err, pools.ErrConflict) {
		return nil, msgErr(http.StatusConflict, "Pool request '%s' already exists with a different spec", in.RequestID)
	}
	if err != nil {
		return nil, msgErr(http.StatusBadRequest, "%v", err)
	}
	return &poolRequestOutput{Body: *r}, nil
}

func (s *Server) getPoolRequest(ctx context.Context, in *poolRequestRef) (*poolRequestGetOutput, error) {
	r, err := s.Pools.GetRequest(ctx, in.UserID, in.RequestID)
	if errors.Is(err, pools.ErrNotFound) {
		return &poolRequestGetOutput{Body: map[string]any{}}, nil
	}
	if err != nil {
		return nil, s.internal(err)
	}
	return &poolRequestGetOutput{Body: r}, nil
}

func (s *Server) deletePoolRequest(ctx context.Context, in *poolRequestRef) (*requestMsgOutput, error) {
	out := &requestMsgOutput{Status: http.StatusOK, Body: RequestMsg{RequestID: in.RequestID, Msg: "Pool request was deleted successfully"}}
	switch err := s.Pools.DeleteRequest(ctx, in.UserID, in.RequestID); {
	case errors.Is(err, pools.ErrNotFound):
		out.Status, out.Body.Msg = http.StatusNotFound, "Pool request was not found"
	case errors.Is(err, pools.ErrCompleted):
		out.Status, out.Body.Msg = http.StatusForbidden, "Pool request was already completed, Could not be deleted"
	case err != nil:
		s.Log.Error("delete pool request", "error", err)
		out.Status, out.Body.Msg = http.StatusInternalServerError, err.Error()
	}
	return out, nil
}

// deletePool moves every node of the pool back to the default pool.
func (s *Server) deletePool(ctx context.Context, in *deletePoolInput) (*msgOutput, error) {
	out := &msgOutput{Status: http.StatusOK, Body: MsgBody{Msg: fmt.Sprintf("Pool '%s' was deleted successfully", in.PoolID)}}
	if _, err := s.Pools.DeletePool(ctx, in.PoolID); err != nil {
		s.Log.Error("delete pool", "pool", in.PoolID, "error", err)
		out.Status = http.StatusInternalServerError
		if in.PoolID == naming.DefaultPool || in.PoolID == pools.NoPool {
			out.Status = http.StatusBadRequest
		}
		out.Body.Msg = fmt.Sprintf("Pool '%s' was deleted failed, error: %v", in.PoolID, err)
	}
	return out, nil
}

// --- resources ---

type resourcesInput struct {
	PoolID         string `query:"pool_id"`
	IncludeDetails bool   `query:"include_details"`
}

type resourceInput struct {
	ResourceID     string `path:"resourceID"`
	PoolID         string `query:"pool_id"`
	IncludeDetails bool   `query:"include_details"`
}

type ResourceList struct {
	Resources []capacity.ModelUsage `json:"resources"`
}
type resourcesOutput struct{ Body ResourceList }

func (s *Server) resources(ctx context.Context, resourceID, pool string, details bool) (*resourcesOutput, error) {
	cfg, err := s.Jobs.Config(ctx)
	if err != nil {
		return nil, s.internal(err)
	}
	nodes, err := s.Capacity.Nodes(ctx, pool)
	if err != nil {
		return nil, s.internal(err)
	}
	return &resourcesOutput{Body: ResourceList{Resources: capacity.Models(cfg, nodes, resourceID, details)}}, nil
}

func (s *Server) registerPlatform(api huma.API) {
	security := []map[string][]string{{"apiKey": {}}}
	op := func(id, method, path, summary string, tag string) huma.Operation {
		return huma.Operation{OperationID: id, Method: method, Path: path, Summary: summary, Tags: []string{tag}, Security: security}
	}

	huma.Register(api, op("warmup-images", http.MethodPost, podPrefix+"/images/warmup",
		"Pre-pull images on training nodes", "images"), s.warmup)
	huma.Register(api, op("list-images", http.MethodGet, podUsersPrefix+"/{userID}/images",
		"List all user images", "images"), s.listImages)

	vol := podUsersPrefix + "/{userID}/volumes"
	huma.Register(api, op("create-volume", http.MethodPost, vol, "Create a new volume", "volumes"), s.createVolume)
	huma.Register(api, op("list-volumes", http.MethodGet, vol, "List all user volumes", "volumes"), s.listVolumes)
	huma.Register(api, op("get-volume", http.MethodGet, vol+"/{volumeID}", "Get a volume", "volumes"), s.getVolume)
	huma.Register(api, op("delete-volume", http.MethodDelete, vol+"/{volumeID}", "Delete a volume", "volumes"), s.deleteVolume)
	huma.Register(api, op("resize-volume", http.MethodPost, vol+"/{volumeID}/resize", "Resize a volume", "volumes"), s.resizeVolume)
	huma.Register(api, op("get-volume-usage", http.MethodGet, vol+"/{volumeID}/usage", "Get volume usage", "volumes"), s.volumeUsage)

	huma.Register(api, op("list-pools", http.MethodGet, poolPrefix+"/pools", "List all available pools", "pools"), s.listPools)
	huma.Register(api, op("list-pool-pods", http.MethodGet, poolPrefix+"/{pool_id}/pods",
		"List all pods and jobs in a pool", "pools"), s.poolPods)

	req := poolRequestPrefix + "/{userID}/pools/requests"
	huma.Register(api, op("create-pool-request", http.MethodPost, req,
		"Request to move nodes into a pool", "pool requests"), s.createPoolRequest)
	huma.Register(api, op("get-pool-request", http.MethodGet, req+"/{request_id}",
		"Get the status of a pool request", "pool requests"), s.getPoolRequest)
	huma.Register(api, op("delete-pool-request", http.MethodDelete, req+"/{request_id}",
		"Delete a pending pool request", "pool requests"), s.deletePoolRequest)
	huma.Register(api, op("delete-pool", http.MethodDelete, poolRequestPrefix+"/{userID}/pools/{poolID}",
		"Delete a pool (move its nodes back to default)", "pool requests"), s.deletePool)

	huma.Register(api, op("list-resources", http.MethodGet, podPrefix+"/resources",
		"List availability of all resource models", "resources"),
		func(ctx context.Context, in *resourcesInput) (*resourcesOutput, error) {
			return s.resources(ctx, "", in.PoolID, in.IncludeDetails)
		})
	huma.Register(api, op("get-resource", http.MethodGet, podPrefix+"/resources/{resourceID}",
		"Get availability of one resource model", "resources"),
		func(ctx context.Context, in *resourceInput) (*resourcesOutput, error) {
			return s.resources(ctx, in.ResourceID, in.PoolID, in.IncludeDetails)
		})
}
