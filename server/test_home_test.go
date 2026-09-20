package server

import (
	"testing"

	"github.com/borism/ollama-cluster/envconfig"
)

func setTestHome(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("OLLAMA_MODELS", "")
	envconfig.ReloadServerConfig()
}
