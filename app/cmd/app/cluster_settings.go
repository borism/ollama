//go:build windows || darwin

package main

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/borism/ollama-cluster/api"
	"github.com/borism/ollama-cluster/envconfig"
)

// The menu-bar items show and change the same cluster settings as the
// Settings page and `ollama cluster`: the app's `ollama serve` owns them
// (GET/POST /api/cluster/config, saved in ~/.ollama/server.json).
var clusterMenu struct {
	sync.Mutex
	cfg     api.ClusterConfig
	pending int // changes still being applied; cfg shows them already
}

// clusterMenuState returns the settings to show, refreshed from the server
// unless a change is still being applied. ok is false while the server
// can't be reached (e.g. still starting). Called on the main thread as the
// menu opens, so the request is kept short.
func clusterMenuState() (cfg api.ClusterConfig, ok bool) {
	clusterMenu.Lock()
	if clusterMenu.pending > 0 {
		defer clusterMenu.Unlock()
		return clusterMenu.cfg, true
	}
	clusterMenu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	fresh, err := api.NewClient(envconfig.Host(), http.DefaultClient).ClusterConfig(ctx)
	if err != nil {
		slog.Debug("cluster menu: server not reachable", "error", err)
		return api.ClusterConfig{}, false
	}

	clusterMenu.Lock()
	defer clusterMenu.Unlock()
	if clusterMenu.pending == 0 {
		clusterMenu.cfg = *fresh
	}
	return clusterMenu.cfg, true
}

// updateClusterConfig shows a change in the menu right away and sends it
// to the server in the background: turning cluster mode or sharing on
// waits for the RPC worker to start (the first time on Apple Silicon, it
// compiles Metal kernels for ~20s), and menu actions run on the main
// thread. If the server rejects it, the next menu open shows the real
// state again.
func updateClusterConfig(req api.ClusterConfigRequest, show func(*api.ClusterConfig)) {
	clusterMenu.Lock()
	show(&clusterMenu.cfg)
	clusterMenu.pending++
	clusterMenu.Unlock()

	go func() {
		cfg, err := api.NewClient(envconfig.Host(), http.DefaultClient).UpdateClusterConfig(context.Background(), &req)
		clusterMenu.Lock()
		defer clusterMenu.Unlock()
		clusterMenu.pending--
		if err != nil {
			slog.Error("failed to change cluster settings", "error", err)
			return
		}
		if clusterMenu.pending == 0 {
			clusterMenu.cfg = *cfg
		}
	}()
}
