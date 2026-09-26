// No build tag -- see cluster_gpu_name.go.

package server

import "testing"

func TestClusterGPUName(t *testing.T) {
	tests := []struct {
		in   []string
		want string
	}{
		{nil, ""},
		{[]string{"cpu"}, ""},
		{[]string{"Apple M2 Max"}, "Apple M2 Max GPU"},
		{[]string{"Some Vendor GPU"}, "Some Vendor GPU"},
		{[]string{"NVIDIA RTX A4500", "NVIDIA RTX A4500"}, "2 NVIDIA RTX A4500 GPUs"},
		{[]string{"NVIDIA RTX A4500", "AMD Radeon Pro 5500M"}, "NVIDIA RTX A4500 and AMD Radeon Pro 5500M GPUs"},
		{[]string{"A", "B", "A", "C"}, "2 A, B and C GPUs"},
	}
	for _, tt := range tests {
		if got := ClusterGPUName(tt.in); got != tt.want {
			t.Errorf("ClusterGPUName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
