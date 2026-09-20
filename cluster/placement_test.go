package cluster

import (
	"testing"

	"github.com/ollama/ollama/api"
	"github.com/ollama/ollama/ml"
)

func gpu(freeMB uint64) ml.DeviceInfo {
	return ml.DeviceInfo{FreeMemory: freeMB * 1024 * 1024}
}

func peer(addr string, rpcPort int, protoMajor, protoMinor int, freeMB uint64) Peer {
	return Peer{
		Addr:       addr,
		RPCPort:    rpcPort,
		ProtoMajor: protoMajor,
		ProtoMinor: protoMinor,
		Devices:    []ml.DeviceInfo{gpu(freeMB)},
	}
}

func mib(n uint64) uint64 { return n * 1024 * 1024 }

func TestSelectRPCServers(t *testing.T) {
	cases := []struct {
		name     string
		gpus     []ml.DeviceInfo
		predict  uint64
		peers    []Peer
		opts     api.Options
		wantRPCs string
	}{
		{
			name:     "local already fits, no-op",
			gpus:     []ml.DeviceInfo{gpu(8000)},
			predict:  mib(4000),
			peers:    []Peer{peer("10.0.0.2", 50052, RPCProtoMajor, 0, 20000)},
			wantRPCs: "",
		},
		{
			name:     "one peer covers the shortfall",
			gpus:     []ml.DeviceInfo{gpu(2000)},
			predict:  mib(4000),
			peers:    []Peer{peer("10.0.0.2", 50052, RPCProtoMajor, 0, 20000)},
			wantRPCs: "10.0.0.2:50052",
		},
		{
			name:    "multiple peers needed",
			gpus:    []ml.DeviceInfo{gpu(1000)},
			predict: mib(10000),
			peers: []Peer{
				peer("10.0.0.2", 50052, RPCProtoMajor, 0, 6000),
				peer("10.0.0.3", 50052, RPCProtoMajor, 0, 5000),
				peer("10.0.0.4", 50052, RPCProtoMajor, 0, 1000),
			},
			// local available = 1000 MiB - 457 MiB MinimumMemory reserve = 543 MiB.
			// sorted descending by free memory: .2 (6000), .3 (5000), .4 (1000)
			// 543 + 6000 = 6543 < 10000, keep going
			// 6543 + 5000 = 11543 >= 10000, stop before adding .4
			wantRPCs: "10.0.0.2:50052,10.0.0.3:50052",
		},
		{
			name:    "wrong ProtoMajor is skipped",
			gpus:    []ml.DeviceInfo{gpu(1000)},
			predict: mib(10000),
			peers: []Peer{
				peer("10.0.0.2", 50052, RPCProtoMajor+1, 0, 50000),
				peer("10.0.0.3", 50052, RPCProtoMajor, 0, 9000),
			},
			wantRPCs: "10.0.0.3:50052",
		},
		{
			name:    "RPCPort zero is skipped",
			gpus:    []ml.DeviceInfo{gpu(1000)},
			predict: mib(10000),
			peers: []Peer{
				peer("10.0.0.2", 0, RPCProtoMajor, 0, 50000),
				peer("10.0.0.3", 50052, RPCProtoMajor, 0, 9000),
			},
			wantRPCs: "10.0.0.3:50052",
		},
		{
			name:     "explicit RPCServers already set is left untouched",
			gpus:     []ml.DeviceInfo{gpu(100)},
			predict:  mib(10000),
			peers:    []Peer{peer("10.0.0.2", 50052, RPCProtoMajor, 0, 50000)},
			opts:     api.Options{Runner: api.Runner{RPCServers: "1.2.3.4:9999"}},
			wantRPCs: "1.2.3.4:9999",
		},
		{
			name:     "zero usable peers leaves RPCServers empty",
			gpus:     []ml.DeviceInfo{gpu(100)},
			predict:  mib(10000),
			peers:    []Peer{peer("10.0.0.2", 0, RPCProtoMajor, 0, 50000)},
			wantRPCs: "",
		},
		{
			name:    "partial help still added when no peer covers it fully",
			gpus:    []ml.DeviceInfo{gpu(100)},
			predict: mib(100000),
			peers: []Peer{
				peer("10.0.0.2", 50052, RPCProtoMajor, 0, 5000),
				peer("10.0.0.3", 50052, RPCProtoMajor, 0, 3000),
			},
			wantRPCs: "10.0.0.2:50052,10.0.0.3:50052",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := SelectRPCServers(c.gpus, c.predict, c.peers, c.opts)
			if got.RPCServers != c.wantRPCs {
				t.Errorf("RPCServers = %q, want %q", got.RPCServers, c.wantRPCs)
			}
		})
	}
}
