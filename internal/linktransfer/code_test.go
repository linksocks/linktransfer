package linktransfer

import "testing"

func TestGetRandomCode_Length(t *testing.T) {
	c := getRandomCode()
	if len(c) < 6 {
		t.Fatalf("code too short: %q", c)
	}
}
