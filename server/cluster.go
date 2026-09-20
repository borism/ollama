package server

import (
	"context"
	"log/slog"
	"time"

	"github.com/ollama/ollama/cluster"
	"github.com/ollama/ollama/discover"
	"github.com/ollama/ollama/envconfig"
	"github.com/ollama/ollama/llm"
	"github.com/ollama/ollama/ml"
)

// startCluster wires OLLAMA_CLUSTER into a running Scheduler: LAN peer
// discovery (cluster.Start) always, and donating this instance's spare GPU
// capacity as an RPC worker (llm.StartRPCWorker) unless OLLAMA_CLUSTER_SHARE
// is set false.
func startCluster(ctx context.Context, sched *Scheduler) {
	rpcPort := func() int { return 0 }
	if envconfig.ClusterShare(true) {
		worker, err := llm.StartRPCWorker(0, envconfig.Models())
		if err != nil {
			slog.Warn("cluster: failed to start RPC worker, this instance will not share GPU capacity", "error", err)
		} else {
			rpcPort = worker.Port
			go func() {
				<-ctx.Done()
				worker.Stop()
			}()
		}
	}

	table, err := cluster.Start(ctx, cluster.Config{
		Port:        int(envconfig.ClusterPort()),
		Interval:    5 * time.Second,
		TTL:         15 * time.Second,
		SelfDevices: func() []ml.DeviceInfo { return discover.GPUDevices(ctx, nil) },
		SelfRPCPort: rpcPort,
		SelfLoad:    sched.clusterLoad,
	})
	if err != nil {
		slog.Warn("cluster: failed to start discovery", "error", err)
		return
	}
	sched.clusterTable = table
	go logClusterPeers(ctx, table)
}

// logClusterPeers is operability, not correctness: cluster.Table has no
// on-change hook, so this just polls Peers() -- fine at a 10s cadence, only
// running when OLLAMA_CLUSTER=1.
func logClusterPeers(ctx context.Context, table *cluster.Table) {
	seen := map[string]bool{}
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := map[string]bool{}
			for _, p := range table.Peers() {
				now[p.ID] = true
				if !seen[p.ID] {
					slog.Info("cluster: peer discovered", "id", p.ID, "addr", p.Addr, "rpc_port", p.RPCPort, "devices", len(p.Devices))
				}
			}
			for id := range seen {
				if !now[id] {
					slog.Info("cluster: peer expired", "id", id)
				}
			}
			seen = now
		}
	}
}
