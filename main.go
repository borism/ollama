package main

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/borism/ollama-cluster/cmd"
)

func main() {
	cobra.CheckErr(cmd.NewCLI().ExecuteContext(context.Background()))
}
