package swiftward

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const ghcrImage = "ghcr.io/disciplinedware/ai-trading-agents/swiftward-server:latest"

// TestPolicyEvals runs the declarative eval suite against the trading policy.
// Uses swiftward-server:local if built, otherwise pulls from GHCR.
// Skipped when docker is not available (CI without docker, unit-only runs).
func TestPolicyEvals(t *testing.T) {
	if err := exec.Command("docker", "info").Run(); err != nil {
		t.Skip("docker not available, skipping policy eval test")
	}
	image := swiftwardImage(t)

	// Resolve rulesets path relative to this test file
	_, testFile, _, _ := runtime.Caller(0)
	rulesetsPath, err := filepath.Abs(filepath.Join(filepath.Dir(testFile), "../../../swiftward/policies/rulesets"))
	if err != nil {
		t.Fatalf("failed to resolve rulesets path: %v", err)
	}

	cmd := exec.Command("docker", "run", "--rm",
		"-v", rulesetsPath+":/app/policies/rulesets",
		image,
		"/app/server", "eval",
		"--ruleset", "trading",
		"--version", "v1",
		"--rulesets-root", "/app/policies/rulesets",
	)

	out, err := cmd.CombinedOutput()
	outStr := string(out)
	t.Logf("eval output:\n%s", outStr)

	if err != nil {
		t.Fatalf("policy eval failed: %v\noutput:\n%s", err, outStr)
	}

	if !strings.Contains(outStr, "ok\t") {
		t.Fatalf("eval output missing success line\noutput:\n%s", outStr)
	}
}

// swiftwardImage returns swiftward-server:local if available, otherwise the GHCR image.
func swiftwardImage(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("docker", "images", "-q", "swiftward-server:local").Output()
	if err == nil && strings.TrimSpace(string(out)) != "" {
		return "swiftward-server:local"
	}
	t.Logf("swiftward-server:local not found, using GHCR image")
	return ghcrImage
}
