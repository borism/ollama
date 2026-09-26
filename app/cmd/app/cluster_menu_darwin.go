package main

// #include <stdbool.h>
import "C"

import (
	"github.com/borism/ollama-cluster/api"
	"github.com/borism/ollama-cluster/app/server"
)

// Exported to cluster_menu_darwin.m, which draws the cluster-mode items in
// the menu-bar menu. See cluster_settings.go for where the settings live.

// ClusterMenuState fills in the settings the menu shows and whether each is
// locked by an OLLAMA_CLUSTER* environment variable on the server (the
// menu can't change those). Returns false while the server isn't reachable.
//
//export ClusterMenuState
func ClusterMenuState(enabled, share, greedy, enabledLocked, shareLocked, placementLocked *C.bool) C.bool {
	cfg, ok := clusterMenuState()
	*enabled = C.bool(cfg.Enabled)
	*share = C.bool(cfg.Share)
	*greedy = C.bool(cfg.Placement == "greedy")
	*enabledLocked = C.bool(cfg.Sources["enabled"] == "env")
	*shareLocked = C.bool(cfg.Sources["share"] == "env")
	*placementLocked = C.bool(cfg.Sources["placement"] == "env")
	return C.bool(ok)
}

//export SetClusterModeEnabled
func SetClusterModeEnabled(enabled C.bool) {
	b := bool(enabled)
	updateClusterConfig(api.ClusterConfigRequest{Enabled: &b}, func(c *api.ClusterConfig) { c.Enabled = b })
}

//export SetClusterShareEnabled
func SetClusterShareEnabled(share C.bool) {
	b := bool(share)
	updateClusterConfig(api.ClusterConfigRequest{Share: &b}, func(c *api.ClusterConfig) { c.Share = b })
}

//export SetClusterPlacementGreedy
func SetClusterPlacementGreedy(greedy C.bool) {
	p := "waterfill"
	if greedy {
		p = "greedy"
	}
	updateClusterConfig(api.ClusterConfigRequest{Placement: &p}, func(c *api.ClusterConfig) { c.Placement = p })
}

// ClusterGPUName returns server.ClusterGPU as a C string the caller frees.
//
//export ClusterGPUName
func ClusterGPUName() *C.char {
	return C.CString(server.ClusterGPU())
}
