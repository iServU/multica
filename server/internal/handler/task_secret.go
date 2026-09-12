package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

var errTaskSecretLease = errors.New("task secret lease unavailable")

type taskSecretLease struct {
	taskID, runtimeID string
	prepared, used    bool
	expiresAt         time.Time
}

// TaskSecretLeaseStore retains only opaque lease state. Secret values are
// passed directly to the authenticated daemon hub and are never stored here.
type TaskSecretLeaseStore struct {
	mu     sync.Mutex
	leases map[string]taskSecretLease
}

func NewTaskSecretLeaseStore() *TaskSecretLeaseStore {
	return &TaskSecretLeaseStore{leases: make(map[string]taskSecretLease)}
}

func (s *TaskSecretLeaseStore) Create(taskID, runtimeID string) string {
	if s == nil || taskID == "" || runtimeID == "" {
		return ""
	}
	leaseID := randomID()
	s.mu.Lock()
	s.leases[leaseID] = taskSecretLease{taskID: taskID, runtimeID: runtimeID, expiresAt: time.Now().Add(10 * time.Minute)}
	s.mu.Unlock()
	return leaseID
}

func (s *TaskSecretLeaseStore) Prepare(leaseID, taskID, runtimeID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	lease, ok := s.leases[leaseID]
	if !ok || lease.taskID != taskID || lease.runtimeID != runtimeID || lease.used || time.Now().After(lease.expiresAt) {
		return errTaskSecretLease
	}
	lease.prepared = true
	s.leases[leaseID] = lease
	return nil
}

func (s *TaskSecretLeaseStore) Consume(leaseID, taskID, runtimeID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	lease, ok := s.leases[leaseID]
	if !ok || lease.taskID != taskID || lease.runtimeID != runtimeID || !lease.prepared || lease.used || time.Now().After(lease.expiresAt) {
		return errTaskSecretLease
	}
	lease.used = true
	s.leases[leaseID] = lease
	return nil
}

func (s *TaskSecretLeaseStore) Release(leaseID, taskID, runtimeID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if lease, ok := s.leases[leaseID]; ok && lease.taskID == taskID && lease.runtimeID == runtimeID {
		lease.used = false
		s.leases[leaseID] = lease
	}
}

// Terminal drops the opaque lease immediately. A configured revoker can use
// this hook to revoke the broker's upstream credential without receiving the
// credential value from Multica.
func (s *TaskSecretLeaseStore) Terminal(taskID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for leaseID, lease := range s.leases {
		if lease.taskID == taskID {
			delete(s.leases, leaseID)
			return true
		}
	}
	return false
}

func (h *Handler) terminalTaskSecret(taskID string) {
	if h.TaskSecretLeases != nil && h.TaskSecretLeases.Terminal(taskID) && h.TaskSecretRevoker != nil {
		h.TaskSecretRevoker(taskID)
	}
}

func (h *Handler) PrepareTaskSecretDestination(w http.ResponseWriter, r *http.Request) {
	if h.TaskSecretLeases == nil {
		writeError(w, http.StatusNotImplemented, "task secret leases unavailable")
		return
	}
	runtimeID, taskID := chi.URLParam(r, "runtimeId"), chi.URLParam(r, "taskId")
	var req struct{ LeaseID string `json:"lease_id"` }
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.LeaseID == "" {
		writeError(w, http.StatusBadRequest, "lease_id is required")
		return
	}
	if err := h.TaskSecretLeases.Prepare(req.LeaseID, taskID, runtimeID); err != nil {
		writeError(w, http.StatusConflict, "task secret destination is not eligible")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) AttachTaskSecret(w http.ResponseWriter, r *http.Request) {
	if h.TaskSecretLeases == nil || h.DaemonHub == nil {
		writeError(w, http.StatusNotImplemented, "task secret leases unavailable")
		return
	}
	runtimeID, taskID := chi.URLParam(r, "runtimeId"), chi.URLParam(r, "taskId")
	var req struct {
		LeaseID string `json:"lease_id"`
		EnvKey  string `json:"env_key"`
		Secret  string `json:"secret"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.LeaseID == "" || req.EnvKey == "" || req.Secret == "" {
		writeError(w, http.StatusBadRequest, "lease_id, env_key, and secret are required")
		return
	}
	if req.EnvKey != "FORGEJO_TOKEN" {
		writeError(w, http.StatusBadRequest, "unsupported task secret environment key")
		return
	}
	if err := h.TaskSecretLeases.Consume(req.LeaseID, taskID, runtimeID); err != nil {
		writeError(w, http.StatusConflict, "task secret lease is not ready or already used")
		return
	}
	if !h.DaemonHub.DeliverTaskSecret(runtimeID, taskID, req.LeaseID, req.EnvKey, req.Secret) {
		h.TaskSecretLeases.Release(req.LeaseID, taskID, runtimeID)
		writeError(w, http.StatusGone, "daemon control channel unavailable")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
