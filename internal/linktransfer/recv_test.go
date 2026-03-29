package linktransfer

import "testing"

func TestNormalizeRecvError_InvalidCodeHints(t *testing.T) {
	cases := []error{
		errorString("comm.NewConnection failed: socks connect tcp: dial tcp 127.0.0.1:0: connect: connection refused"),
		errorString("could not connect to localhost:53100: comm.NewConnection failed"),
		errorString("message authentication failed"),
		errorString("bad password"),
	}

	for _, in := range cases {
		err := normalizeRecvError("abc123", in)
		if err == nil {
			t.Fatalf("expected normalized error for %q", in.Error())
		}
		if err.Error() != "invalid code or sender is unavailable (code: abc123)" {
			t.Fatalf("unexpected normalized error: %q", err.Error())
		}
	}
}

func TestNormalizeRecvError_KeepUnrelatedError(t *testing.T) {
	in := errorString("permission denied")
	out := normalizeRecvError("abc123", in)
	if out == nil || out.Error() != in.Error() {
		t.Fatalf("expected unrelated error to pass through, got: %v", out)
	}
}

type errorString string

func (e errorString) Error() string { return string(e) }
