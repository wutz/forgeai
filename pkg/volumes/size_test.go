package volumes

import "testing"

func TestCheckSizeBounds(t *testing.T) {
	for _, tc := range []struct {
		size int64
		ok   bool
	}{
		{1, true},
		{maxSizeGiB, true},
		{0, false},
		{-5, false},
		{maxSizeGiB + 1, false},
		{1 << 33, false}, // would overflow sizeGiB<<30 in int64 arithmetic
	} {
		if err := checkSize(tc.size); (err == nil) != tc.ok {
			t.Errorf("checkSize(%d) = %v, want ok=%v", tc.size, err, tc.ok)
		}
	}
}
