package app

import "testing"

// A GPU pod with no product label is counted as unclassed, not under a class
// named "", which would render as an unnamed row in the quota table.
func TestGPUUsageClass(t *testing.T) {
	for _, tt := range []struct{ product, want string }{
		{"", "*"},
		{"NVIDIA-GeForce-RTX-5090", "rtx-5090"},
		{"tesla-a100", "a100"},
	} {
		if got := gpuUsageClass(tt.product); got != tt.want {
			t.Errorf("gpuUsageClass(%q) = %q, want %q", tt.product, got, tt.want)
		}
	}
}
