package mlxrunner

import (
	"github.com/borism/ollama-cluster/mlxrunner/model"
)

// SupportsArchitecture reports whether the MLX runner has a constructor for arch.
func SupportsArchitecture(arch string) bool {
	return model.SupportsArchitecture(arch)
}

// SupportsDraftArchitecture reports whether the MLX runner has a draft constructor for arch.
func SupportsDraftArchitecture(arch string) bool {
	return model.SupportsDraftArchitecture(arch)
}
