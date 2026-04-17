package testutil

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestCILockout_PanicsWithAPIKeys(t *testing.T) {
	// The init() already ran in this process (without CI=true),
	// so we test via subprocess re-invocation.
	cmd := exec.Command(os.Args[0], "-test.run=TestCILockout_PanicsWithAPIKeys")
	cmd.Env = []string{
		"CI=true",
		"DASHSCOPE_API_KEY=fake-key",
		"HOME=" + os.Getenv("HOME"),
		"PATH=" + os.Getenv("PATH"),
		"GOPATH=" + os.Getenv("GOPATH"),
		"CI_LOCKOUT_SUBPROCESS=1",
	}

	output, err := cmd.CombinedOutput()
	if os.Getenv("CI_LOCKOUT_SUBPROCESS") == "1" {
		// We're in the subprocess — init() should have panicked before we get here.
		// If we reach this line, the panic didn't fire.
		t.Fatal("expected init() to panic but it did not")
		return
	}

	if err == nil {
		t.Fatal("expected subprocess to fail due to init panic")
	}
	if !strings.Contains(string(output), "API keys must not be set in CI environment") {
		t.Errorf("expected panic message in output, got:\n%s", output)
	}
}

func TestCILockout_NoPanicWithoutCI(t *testing.T) {
	// Verify that the current process (CI != "true") didn't panic.
	// If we reach this test, init() ran without panicking — that's the test.
	if os.Getenv("CI") == "true" {
		t.Skip("skipping in CI environment")
	}
}
