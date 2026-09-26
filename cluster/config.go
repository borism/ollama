package cluster

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// ValidateSeeds checks that seeds is empty, or a comma-separated list of
// "host:port" addresses with ports 1-65535 (envconfig.ClusterSeeds splits
// the same string).
func ValidateSeeds(seeds string) error {
	if strings.TrimSpace(seeds) == "" {
		return nil
	}
	for _, seed := range strings.Split(seeds, ",") {
		seed = strings.TrimSpace(seed)
		if seed == "" {
			return fmt.Errorf("empty seed address")
		}
		host, portStr, err := net.SplitHostPort(seed)
		if err != nil {
			return fmt.Errorf("invalid seed %q: expected host:port", seed)
		}
		if host == "" {
			return fmt.Errorf("invalid seed %q: missing host", seed)
		}
		if port, err := strconv.Atoi(portStr); err != nil || port < 1 || port > 65535 {
			return fmt.Errorf("invalid seed %q: port must be 1-65535", seed)
		}
	}
	return nil
}

// ValidatePlacement checks placement is a policy SelectRPCServers knows.
func ValidatePlacement(placement string) error {
	if placement != "waterfill" && placement != "greedy" {
		return fmt.Errorf("invalid placement %q: expected waterfill or greedy", placement)
	}
	return nil
}
