package xray

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// SupportsClientBandwidth verifies the actual executable, rather than inferring
// support from its upstream version. Stock Xray must never silently ignore caps.
func SupportsClientBandwidth() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	body, err := exec.CommandContext(ctx, GetBinaryPath(), "version").Output()
	return err == nil && strings.Contains(string(body), "SorvoRateV1")
}
