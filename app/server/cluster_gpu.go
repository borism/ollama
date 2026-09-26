//go:build windows || darwin

package server

import (
	"context"
	"sync"
	"time"
)

var clusterGPU struct {
	sync.Mutex
	name string
}

// ClusterGPU returns ClusterGPUName for this computer, or "" while `ollama
// serve` hasn't logged its devices yet or when it has no GPU.
// ponytail: cached for the app's lifetime once found, a GPU added while the
// app runs needs an app restart to show up.
func ClusterGPU() string {
	clusterGPU.Lock()
	defer clusterGPU.Unlock()
	if clusterGPU.name != "" {
		return clusterGPU.name
	}

	// Short: the menu calls this while it opens.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	info, err := GetInferenceInfo(ctx)
	if err != nil {
		return ""
	}
	var descriptions []string
	for _, c := range info.Computes {
		descriptions = append(descriptions, c.Description)
	}
	clusterGPU.name = ClusterGPUName(descriptions)
	return clusterGPU.name
}
