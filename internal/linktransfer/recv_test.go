package linktransfer

import (
	"context"
	"errors"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"
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

	done := watchPeerDisconnect(ctx, trt, cancel, "sender")
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
