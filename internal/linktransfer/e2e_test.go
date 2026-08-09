package linktransfer

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/linksocks/linksocks/linksocks"
)

const e2eTunnelToken = "lt-e2e-tunnel"

func startLocalWSRelay(t *testing.T) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve ws port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	srv := linksocks.NewLinkSocksServer(linksocks.DefaultServerOption().
		WithWSHost("127.0.0.1").
		WithWSPort(port).
		WithLogger(newLinksocksLogger(false)))
	if err := srv.WaitReady(ctx, 10*time.Second); err != nil {
		cancel()
		t.Fatalf("start local ws relay: %v", err)
	}
	if _, err := srv.AddReverseToken(&linksocks.ReverseTokenOptions{
		Token:                e2eTunnelToken,
		AllowManageConnector: true,
	}); err != nil {
		cancel()
		t.Fatalf("register tunnel token: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		srv.Close()
	})
	return fmt.Sprintf("ws://127.0.0.1:%d", port)
}

type e2eResult struct {
	sendErr error
	recvErr error
}

func runE2ETransfer(t *testing.T, wsURL, code string, sendArgs, recvArgs []string) e2eResult {
	t.Helper()

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	t.Setenv("LINKSOCKS_TOKEN", e2eTunnelToken)

	common := []string{"--url", wsURL, "--code", code, "--direct-mode", "relay-only"}
	sendCmd := newSendCmd(ctx)
	sendCmd.SetArgs(append(common, sendArgs...))
	recvCmd := newRecvCmd(ctx)
	recvCmd.SetArgs(append(common, recvArgs...))

	sendCh := make(chan error, 1)
	recvCh := make(chan error, 1)
	go func() { sendCh <- sendCmd.Execute() }()
	time.Sleep(500 * time.Millisecond)
	go func() { recvCh <- recvCmd.Execute() }()

	var res e2eResult
	deadline := time.Now().Add(60 * time.Second)
	for completed := 0; completed < 2; {
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("e2e transfer timed out (send: %v, recv: %v)", res.sendErr, res.recvErr)
		}
		select {
		case res.sendErr = <-sendCh:
			completed++
		case res.recvErr = <-recvCh:
			completed++
		case <-time.After(time.Second):
		}
	}
	return res
}

func newE2ECode() string {
	return fmt.Sprintf("e2e-%d", time.Now().UnixNano()%100000000)
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func findFile(t *testing.T, root, name string) string {
	t.Helper()
	found := ""
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && info.Name() == name {
			found = p
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	if found == "" {
		t.Fatalf("file %q not found under %s", name, root)
	}
	return found
}

func findFileOrEmpty(root, name string) string {
	found := ""
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && info.Name() == name {
			found = p
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

func assertRecvFile(t *testing.T, root, name, want string) {
	t.Helper()
	got, err := os.ReadFile(findFile(t, root, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	if string(got) != want {
		t.Fatalf("%s content = %q, want %q", name, got, want)
	}
}

func TestE2EFileTransfer(t *testing.T) {
	wsURL := startLocalWSRelay(t)
	code := newE2ECode()

	srcFile := filepath.Join(t.TempDir(), "payload.bin")
	payload := strings.Repeat("e2e-payload\n", 65536) // ~800KB
	mustWriteFile(t, srcFile, payload)

	recvDir := filepath.Join(t.TempDir(), "out")
	if err := os.MkdirAll(recvDir, 0o755); err != nil {
		t.Fatal(err)
	}

	res := runE2ETransfer(t, wsURL, code, []string{srcFile}, []string{"--out", recvDir})
	if res.sendErr != nil || res.recvErr != nil {
		t.Fatalf("transfer failed: send=%v recv=%v", res.sendErr, res.recvErr)
	}

	got, err := os.ReadFile(filepath.Join(recvDir, "payload.bin"))
	if err != nil {
		t.Fatalf("received file missing: %v", err)
	}
	if string(got) != payload {
		t.Fatal("received content mismatch")
	}
}

func TestE2EZipFolder(t *testing.T) {
	wsURL := startLocalWSRelay(t)
	code := newE2ECode()

	dataDir := filepath.Join(t.TempDir(), "data")
	mustWriteFile(t, filepath.Join(dataDir, "a.txt"), "aaa")
	mustWriteFile(t, filepath.Join(dataDir, "sub", "b.txt"), "bbb")

	recvDir := filepath.Join(t.TempDir(), "out")
	if err := os.MkdirAll(recvDir, 0o755); err != nil {
		t.Fatal(err)
	}

	res := runE2ETransfer(t, wsURL, code, []string{"--zip", dataDir}, []string{"--out", recvDir})
	if res.sendErr != nil || res.recvErr != nil {
		t.Fatalf("zip transfer failed: send=%v recv=%v", res.sendErr, res.recvErr)
	}

	assertRecvFile(t, recvDir, "a.txt", "aaa")
	assertRecvFile(t, recvDir, "b.txt", "bbb")
}

func TestE2EExclude(t *testing.T) {
	wsURL := startLocalWSRelay(t)
	code := newE2ECode()

	dataDir := filepath.Join(t.TempDir(), "data")
	mustWriteFile(t, filepath.Join(dataDir, "keep.txt"), "keep")
	mustWriteFile(t, filepath.Join(dataDir, "skip.log"), "skip")

	recvDir := filepath.Join(t.TempDir(), "out")
	if err := os.MkdirAll(recvDir, 0o755); err != nil {
		t.Fatal(err)
	}

	res := runE2ETransfer(t, wsURL, code, []string{"--exclude", "skip", dataDir}, []string{"--out", recvDir})
	if res.sendErr != nil || res.recvErr != nil {
		t.Fatalf("exclude transfer failed: send=%v recv=%v", res.sendErr, res.recvErr)
	}

	assertRecvFile(t, recvDir, "keep.txt", "keep")
	if p := findFileOrEmpty(recvDir, "skip.log"); p != "" {
		t.Fatalf("excluded file was transferred: %s", p)
	}
}

func TestE2ERecvStdout(t *testing.T) {
	wsURL := startLocalWSRelay(t)
	code := newE2ECode()

	srcFile := filepath.Join(t.TempDir(), "hello.txt")
	mustWriteFile(t, srcFile, "hello e2e\n")

	recvDir := filepath.Join(t.TempDir(), "out")
	if err := os.MkdirAll(recvDir, 0o755); err != nil {
		t.Fatal(err)
	}

	origStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = origStdout })

	outCh := make(chan []byte, 1)
	go func() {
		b, _ := io.ReadAll(r)
		outCh <- b
	}()

	res := runE2ETransfer(t, wsURL, code, []string{srcFile}, []string{"--out", recvDir, "--stdout"})
	w.Close()
	got := <-outCh

	if res.sendErr != nil || res.recvErr != nil {
		t.Fatalf("stdout transfer failed: send=%v recv=%v", res.sendErr, res.recvErr)
	}
	if string(got) != "hello e2e\n" {
		t.Fatalf("stdout content = %q, want %q", got, "hello e2e\n")
	}
	entries, err := os.ReadDir(recvDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("stdout mode should not leave files, found: %v", entries)
	}
}
