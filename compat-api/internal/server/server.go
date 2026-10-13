// Package server implements the scc-k8s-v2 compatible API (jobs, images,
// volumes, pools and resources) on top of forgeai.
package server

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	v1 "github.com/scclabs/forgeai/api/v1alpha1"
	"github.com/scclabs/forgeai/pkg/capacity"
	"github.com/scclabs/forgeai/pkg/images"
	"github.com/scclabs/forgeai/pkg/jobs"
	"github.com/scclabs/forgeai/pkg/pools"
	"github.com/scclabs/forgeai/pkg/volumes"
)

// RootPath is the proxy prefix scc-k8s-v2 was served under (FastAPI root_path).
const RootPath = "/ngc"

const jobsPrefix = "/apis/job/v1alpha2/users"

// msgError renders as {"msg": "..."} like scc-k8s-v2 JSONResponse errors.
type msgError struct {
	Status int    `json:"-"`
	Msg    string `json:"msg"`
}

func (e *msgError) Error() string  { return e.Msg }
func (e *msgError) GetStatus() int { return e.Status }

func notFound(jobID string) error {
	return &msgError{Status: http.StatusNotFound, Msg: fmt.Sprintf("Job '%s' was not found", jobID)}
}

// Server holds dependencies of the handlers.
type Server struct {
	Jobs     *jobs.Client
	Volumes  *volumes.Client
	Pools    *pools.Client
	Capacity *capacity.Client
	Warmer   *images.Warmer
	// Harbor credentials for listing user images.
	RegistryUser, RegistryPassword string
	// VolumeUsageToken authenticates to scc-monitor-querier.
	VolumeUsageToken string
	// APIKeys are accepted X-API-Key values.
	APIKeys []string
	Now     func() time.Time
	Log     *slog.Logger
}

// APIKeysFromEnv returns the values of env vars whose name starts with prefix,
// matching fastapi-key-auth's key_pattern behaviour.
func APIKeysFromEnv(prefix string) []string {
	var keys []string
	for _, kv := range os.Environ() {
		name, val, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, prefix) && val != "" {
			keys = append(keys, val)
		}
	}
	return keys
}

func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Server) authorized(key string) bool {
	for _, k := range s.APIKeys {
		if subtle.ConstantTimeCompare([]byte(k), []byte(key)) == 1 {
			return true
		}
	}
	return false
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Handler returns the HTTP handler serving the API at / and under RootPath.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	cfg := huma.DefaultConfig("forgeai scc-k8s compat API", "0.1.0")
	cfg.Info.Description = "scc-k8s-v2 compatible API backed by forgeai. Interactive access to pods (SSH, Jupyter, exec) is not offered."
	cfg.DocsPath = "/apis/docs"
	cfg.OpenAPIPath = "/apis/openapi"
	cfg.Servers = []*huma.Server{{URL: RootPath}}
	// No $schema field in responses: keep bodies identical to scc-k8s-v2.
	cfg.CreateHooks = nil
	cfg.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"apiKey": {Type: "apiKey", In: "header", Name: "X-API-Key"},
	}
	api := humago.New(mux, cfg)
	s.register(api)
	s.registerPlatform(api)

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p, ok := strings.CutPrefix(r.URL.Path, RootPath); ok && (p == "" || strings.HasPrefix(p, "/")) {
			r.URL.Path = p
			r.URL.RawPath = ""
		}
		if protected(r.URL.Path) {
			key := r.Header.Get("X-API-Key")
			if key == "" {
				// FastAPI's response for a missing required header.
				writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"detail": []map[string]any{{
					"type": "missing", "loc": []string{"header", "x-api-key"}, "msg": "Field required", "input": nil,
				}}})
				return
			}
			if !s.authorized(key) {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"detail": "unauthorized"})
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}

// protected reports whether a path requires an API key: every /apis/ route
// except the OpenAPI docs.
func protected(path string) bool {
	if !strings.HasPrefix(path, "/apis/") {
		return false
	}
	for _, p := range []string{"/apis/docs", "/apis/openapi"} {
		if path == p || strings.HasPrefix(path, p+".") || strings.HasPrefix(path, p+"/") {
			return false
		}
	}
	return true
}

var jobIDPattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

type createInput struct {
	UserID string `path:"userID"`
	JobID  string `query:"jobID" doc:"Job ID; a random one is generated when empty"`
	Method string `query:"method" enum:"create,start" default:"create"`
	Body   JobIn
}

type jobOutput struct{ Body JobOut }

type jobRef struct {
	UserID string `path:"userID"`
	JobID  string `path:"jobID"`
}

type listInput struct {
	UserID string `path:"userID"`
	JobID  string `query:"jobID"`
}

type listOutput struct{ Body JobList }

// getOutput is a JobOut, or {} when the job does not exist (scc-k8s-v2 behaviour).
type getOutput struct{ Body any }

func randomJobID() string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Server) internal(err error) error {
	s.Log.Error("request failed", "error", err)
	return huma.Error500InternalServerError(err.Error())
}

func (s *Server) register(api huma.API) {
	security := []map[string][]string{{"apiKey": {}}}
	tags := []string{"jobs"}

	huma.Register(api, huma.Operation{
		OperationID: "create-job", Method: http.MethodPost, Path: jobsPrefix + "/{userID}/jobs",
		Summary: "Create a new job", Tags: tags, Security: security,
		Description: "Create a job with the specified resource. method=start resumes an existing job.",
	}, s.createJob)

	for _, op := range []struct{ action, summary string }{
		{"stop", "Stop a job and release resources"},
		{"restart", "Restart a job"},
	} {
		action := op.action
		huma.Register(api, huma.Operation{
			OperationID: action + "-job", Method: http.MethodPost, Path: jobsPrefix + "/{userID}/jobs/{jobID}/" + action,
			Summary: op.summary, Tags: tags, Security: security,
		}, func(ctx context.Context, in *jobRef) (*jobOutput, error) {
			return s.operate(ctx, in, action)
		})
	}

	huma.Register(api, huma.Operation{
		OperationID: "delete-job", Method: http.MethodDelete, Path: jobsPrefix + "/{userID}/jobs/{jobID}",
		Summary: "Delete a job", Tags: tags, Security: security,
	}, func(ctx context.Context, in *jobRef) (*jobOutput, error) {
		return s.operate(ctx, in, "delete")
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-jobs", Method: http.MethodGet, Path: jobsPrefix + "/{userID}/jobs",
		Summary: "List all jobs of a user", Tags: tags, Security: security,
	}, s.listJobs)

	huma.Register(api, huma.Operation{
		OperationID: "get-job", Method: http.MethodGet, Path: jobsPrefix + "/{userID}/jobs/{jobID}",
		Summary: "Get one job of a user", Tags: tags, Security: security,
		Description: "Returns the job, or {} when it does not exist.",
	}, s.getJob)
}

func (s *Server) createJob(ctx context.Context, in *createInput) (*jobOutput, error) {
	jobID := in.JobID
	if jobID == "" {
		jobID = randomJobID()
	}
	if !jobIDPattern.MatchString(jobID) || len(jobID) > 40 {
		return nil, &msgError{Status: http.StatusBadRequest, Msg: fmt.Sprintf("invalid jobID %q", jobID)}
	}

	if in.Method == "start" {
		job, err := s.Jobs.Start(ctx, s.Jobs.Name(in.UserID, jobID))
		if errors.Is(err, jobs.ErrNotFound) {
			return nil, notFound(jobID)
		}
		if err != nil {
			return nil, s.internal(err)
		}
		return s.created(job), nil
	}

	cfg, err := s.Jobs.Config(ctx)
	if err != nil {
		return nil, s.internal(err)
	}
	sp := spec(in.UserID, jobID, &in.Body)
	if err := jobs.Validate(cfg, &sp); err != nil {
		return nil, &msgError{Status: http.StatusBadRequest, Msg: err.Error()}
	}
	job, err := s.Jobs.Apply(ctx, sp)
	if err != nil {
		return nil, s.internal(err)
	}
	return s.created(job), nil
}

// created renders a freshly submitted job the way scc-k8s-v2 did: status Pending.
func (s *Server) created(job *v1.TrainingJob) *jobOutput {
	out := toJobOut(job, s.now())
	out.Status = strp("Pending")
	if job.CreationTimestamp.IsZero() {
		out.CreateTime = s.now().Format("2006-01-02 15:04:05")
	}
	return &jobOutput{Body: out}
}

func (s *Server) operate(ctx context.Context, in *jobRef, action string) (*jobOutput, error) {
	name := s.Jobs.Name(in.UserID, in.JobID)
	var job *v1.TrainingJob
	var err error
	switch action {
	case "stop":
		job, err = s.Jobs.Stop(ctx, name)
	case "restart":
		job, err = s.Jobs.Restart(ctx, name)
	case "delete":
		job, err = s.Jobs.Delete(ctx, name)
	}
	if errors.Is(err, jobs.ErrNotFound) {
		return nil, notFound(in.JobID)
	}
	if err != nil {
		return nil, s.internal(err)
	}
	return &jobOutput{Body: toJobOut(job, s.now())}, nil
}

func (s *Server) listJobs(ctx context.Context, in *listInput) (*listOutput, error) {
	items, err := s.Jobs.List(ctx, in.UserID, in.JobID)
	if err != nil {
		return nil, s.internal(err)
	}
	out := JobList{Jobs: []JobOut{}}
	now := s.now()
	for i := range items {
		if !items[i].DeletionTimestamp.IsZero() {
			continue
		}
		out.Jobs = append(out.Jobs, toJobOut(&items[i], now))
	}
	return &listOutput{Body: out}, nil
}

func (s *Server) getJob(ctx context.Context, in *jobRef) (*getOutput, error) {
	job, err := s.Jobs.Get(ctx, in.UserID, in.JobID)
	if errors.Is(err, jobs.ErrNotFound) || (err == nil && !job.DeletionTimestamp.IsZero()) {
		return &getOutput{Body: map[string]any{}}, nil
	}
	if err != nil {
		return nil, s.internal(err)
	}
	return &getOutput{Body: toJobOut(job, s.now())}, nil
}
