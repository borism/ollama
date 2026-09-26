// No build tag, unlike the rest of this package (windows || darwin): this
// file and its test build and run on Linux too.

package server

import (
	"fmt"
	"strings"
)

// ClusterGPUName names this computer's GPUs for the "Share the ... GPU"
// label, from the device descriptions `ollama serve` logs ("inference
// compute", see discover/types.go): "Apple M2 Max GPU", "2 NVIDIA GeForce
// RTX 4090 GPUs", "NVIDIA ... and AMD ... GPUs". Returns "" when there are
// none, including the "cpu" entry a GPU-less machine logs.
func ClusterGPUName(descriptions []string) string {
	var names []string
	count := map[string]int{}
	total := 0
	for _, d := range descriptions {
		d = strings.TrimSuffix(strings.TrimSpace(d), " GPU")
		if d == "" || d == "cpu" {
			continue
		}
		if count[d] == 0 {
			names = append(names, d)
		}
		count[d]++
		total++
	}
	if total == 0 {
		return ""
	}

	for i, n := range names {
		if count[n] > 1 {
			names[i] = fmt.Sprintf("%d %s", count[n], n)
		}
	}
	list := names[len(names)-1]
	if len(names) > 1 {
		list = strings.Join(names[:len(names)-1], ", ") + " and " + list
	}
	if total > 1 {
		return list + " GPUs"
	}
	return list + " GPU"
}
