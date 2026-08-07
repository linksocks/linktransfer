package linktransfer

import (
	"context"
	"errors"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/linksocks/linksocks/linksocks"
)

func TestIsPeerGoneError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "closed connection", err: errors.New("read tcp 127.0.0.1:19698: use of closed network connection"), want: true},
		{name: "reset connection", err: errors.New("read tcp: connection reset by peer"), want: true},
		{name: "closed network", err: net.ErrClosed, want: true},
		{name: "eof", err: io.EOF, want: true},
		{name: "bad password", err: errors.New("password mismatch"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isPeerGoneError(tt.err); got != tt.want {
				t.Fatalf("isPeerGoneError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTransferSessionActive(t *testing.T) {
	if !transferSessionActive(false, 0, errors.New("use of closed network connection")) {
		t.Fatal("closed network connection should keep a transfer retryable")
	}
	if !transferSessionActive(false, 1, errors.New("connection failed")) {
		t.Fatal("a paired tunnel should keep a transfer retryable")
	}
	if transferSessionActive(false, 0, errors.New("bad password")) {
		t.Fatal("an authentication failure should not start persistent retries")
	}
}

func TestTransferResultAfterPeerDisconnect(t *testing.T) {
	peerErr := errors.New("sender is gone")
	if got := transferResultAfterPeerDisconnect(peerErr, context.Canceled); !errors.Is(got, peerErr) {
		t.Fatalf("transferResultAfterPeerDisconnect() = %v, want %v", got, peerErr)
	}
	if got := transferResultAfterPeerDisconnect(peerErr, nil); got != nil {
		t.Fatalf("transferResultAfterPeerDisconnect() = %v, want nil", got)
	}
	transferErr := errors.New("checksum mismatch")
	if got := transferResultAfterPeerDisconnect(peerErr, transferErr); !errors.Is(got, transferErr) {
		t.Fatalf("transferResultAfterPeerDisconnect() = %v, want %v", got, transferErr)
	}
}

func TestRecvCommandHasOverwriteFlag(t *testing.T) {
	cmd := newRecvCmd(context.Background())
	if err := cmd.ParseFlags([]string{"--overwrite"}); err != nil {
		t.Fatalf("ParseFlags() error = %v", err)
	}
	value, err := cmd.Flags().GetBool("overwrite")
	if err != nil {
		t.Fatalf("GetBool(overwrite) error = %v", err)
	}
	if !value {
		t.Fatal("overwrite flag should be enabled")
	}
}

func TestApplyDirectMode(t *testing.T) {
	opt := linksocks.DefaultClientOption()
	if err := applyDirectMode(opt, "relay-only"); err != nil {
		t.Fatalf("applyDirectMode() error = %v", err)
	}
	if opt.DirectMode != linksocks.DirectModeRelayOnly {
		t.Fatalf("applyDirectMode() set %q, want %q", opt.DirectMode, linksocks.DirectModeRelayOnly)
	}

	if err := applyDirectMode(opt, ""); err != nil {
		t.Fatalf("applyDirectMode() with empty mode error = %v", err)
	}
	if opt.DirectMode != linksocks.DirectModeAuto {
		t.Fatalf("empty direct mode set %q, want %q", opt.DirectMode, linksocks.DirectModeAuto)
	}
}

func TestWaitForTransferAfterPeerDisconnectPrefersCompletion(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	done <- nil
	peerErr := errors.New("receiver is gone")

	if got := waitForTransferAfterPeerDisconnect(done, peerErr, cancel); got != nil {
		t.Fatalf("waitForTransferAfterPeerDisconnect() = %v, want nil", got)
	}
	select {
	case <-ctx.Done():
		t.Fatal("completed transfer should not be canceled")
	default:
	}
}

func TestWatchPeerDisconnectIgnoresStaleClosedSignal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	staleDisconnected := make(chan struct{})
	close(staleDisconnected)
	var partners atomic.Int32
	partners.Store(1)

	trt := &tunnelRuntime{
		partnerCount: func() int {
			return int(partners.Load())
		},
		disconnected: func() <-chan struct{} {
			return staleDisconnected
		},
	}

	done := watchPeerDisconnect(ctx, trt, "sender")
	select {
	case err := <-done:
		t.Fatalf("watchPeerDisconnect() returned from stale signal: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	partners.Store(0)
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("watchPeerDisconnect() returned nil error")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("watchPeerDisconnect() did not detect the lost partner")
	}
}
