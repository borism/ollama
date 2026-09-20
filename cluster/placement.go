package cluster

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ollama/ollama/api"
	"github.com/ollama/ollama/envconfig"
	"github.com/ollama/ollama/ml"
)

// RPCProtoMajor/RPCProtoMinor are defined in discovery.go.

// localAvailable is the same "usable free memory" convention
// server/sched.go's load() applies per GPU before deciding placement
// (envconfig.GpuOverhead() + gpu.MinimumMemory() set aside, floored at 0).
func localAvailable(gpus []ml.DeviceInfo) uint64 {
	var total uint64
	overhead := envconfig.GpuOverhead()
	for _, gpu := range gpus {
		reserve := overhead + gpu.MinimumMemory()
		if gpu.FreeMemory > reserve {
			total += gpu.FreeMemory - reserve
		}
	}
	return total
}

// peerFreeMemory is a peer's spillable capacity: the sum of its
// self-reported devices' free memory. No overhead margin -- that's the
// peer's own ggml-rpc-server's problem when it actually allocates.
func peerFreeMemory(p Peer) uint64 {
	var total uint64
	for _, d := range p.Devices {
		total += d.FreeMemory
	}
	return total
}

func usablePeer(p Peer) bool {
	return p.RPCPort != 0 && p.ProtoMajor == RPCProtoMajor && p.ProtoMinor <= RPCProtoMinor
}

// SelectRPCServers decides whether opts should spill onto cluster peers to
// fit a model whose predicted VRAM need is `predicted`, and if so fills in
// opts.RPCServers (see api.Runner.RPCServers, llm/llama_server.go's
// appendRPCArgs -- this function only needs to produce that string, nothing
// downstream needs to change). Pure function: no network calls, just a
// decision over the snapshot it's given.
//
// Memory-proportional only (the project's planner.py `best_device`
// fallback mode, ported): peers are ranked by free memory and added
// greedily until the shortfall is covered. No speed-aware water-fill --
// that needs bench data this function doesn't have.
func SelectRPCServers(gpus []ml.DeviceInfo, predicted uint64, peers []Peer, opts api.Options) api.Options {
	if opts.RPCServers != "" {
		return opts
	}

	local := localAvailable(gpus)
	if local >= predicted {
		return opts
	}

	usable := make([]Peer, 0, len(peers))
	for _, p := range peers {
		if usablePeer(p) {
			usable = append(usable, p)
		}
	}
	if len(usable) == 0 {
		return opts
	}

	sort.SliceStable(usable, func(i, j int) bool {
		return peerFreeMemory(usable[i]) > peerFreeMemory(usable[j])
	})

	cum := local
	addrs := make([]string, 0, len(usable))
	for _, p := range usable {
		if cum >= predicted {
			break
		}
		addrs = append(addrs, fmt.Sprintf("%s:%d", p.Addr, p.RPCPort))
		cum += peerFreeMemory(p)
	}

	opts.RPCServers = strings.Join(addrs, ",")
	return opts
}
