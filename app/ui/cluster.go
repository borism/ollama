//go:build windows || darwin

package ui

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/borism/ollama-cluster/api"
	"github.com/borism/ollama-cluster/app/server"
)

// clusterSettingsResponse is what both cluster endpoints return: the app's
// `ollama serve` cluster settings (GET/POST /api/cluster/config, saved in
// its ~/.ollama/server.json -- the same ones `ollama cluster` changes)
// plus the local GPU name for the sharing switch's label.
type clusterSettingsResponse struct {
	api.ClusterConfig
	GPU string `json:"gpu"`
}

func (s *Server) getClusterSettings(w http.ResponseWriter, r *http.Request) error {
	cfg, err := s.inferenceClient().ClusterConfig(r.Context())
	if err != nil {
		return fmt.Errorf("failed to load cluster settings: %w", err)
	}

	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(clusterSettingsResponse{*cfg, server.ClusterGPU()})
}

// clusterSettings passes changed settings on to the server, which
// validates, saves and applies them without a restart. Fields the request
// leaves out keep their value.
func (s *Server) clusterSettings(w http.ResponseWriter, r *http.Request) error {
	var req api.ClusterConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return fmt.Errorf("invalid request body: %w", err)
	}

	cfg, err := s.inferenceClient().UpdateClusterConfig(r.Context(), &req)
	var statusErr api.StatusError
	if errors.As(err, &statusErr) && statusErr.StatusCode == http.StatusBadRequest {
		http.Error(w, statusErr.ErrorMessage, http.StatusBadRequest)
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to save cluster settings: %w", err)
	}

	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(clusterSettingsResponse{*cfg, server.ClusterGPU()})
}
