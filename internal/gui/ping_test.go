package gui

import (
	"os"
	"testing"
)

func TestMeasurePing(t *testing.T) {
	host := os.Getenv("CIPHER_PROXY_TEST_HOST")
	if host == "" {
		t.Skip("CIPHER_PROXY_TEST_HOST not set; skipping ping test (no hardcoded target)")
	}
	ms, err := measurePing(host)
	if err != nil {
		t.Skipf("ping target %s unreachable in test env: %v", host, err)
	}
	if ms < 0 {
		t.Fatalf("unexpected negative ping: %d", ms)
	}
	t.Logf("ping %s = %d ms", host, ms)
}
