package netease

import "testing"

func TestNormalizeQuality(t *testing.T) {
	if got := NormalizeQuality("hires"); got != "hires" {
		t.Errorf("NormalizeQuality(hires) = %q", got)
	}
	if got := NormalizeQuality("bogus"); got != DefaultQuality {
		t.Errorf("NormalizeQuality(bogus) = %q, 期望 %q", got, DefaultQuality)
	}
}
