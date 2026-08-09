package linktransfer

import (
	"context"
	"reflect"
	"testing"
)

func TestBuildSendOptionsDefaults(t *testing.T) {
	ops := buildSendOptions("test-code-123", "", false, false, false, false, false, false, nil, "")

	if !ops.IsSender {
		t.Fatal("sender options should set IsSender")
	}
	if ops.SharedSecret != "test-code-123" {
		t.Fatalf("SharedSecret = %q, want %q", ops.SharedSecret, "test-code-123")
	}
	if ops.SendingText {
		t.Fatal("SendingText should be false without --text")
	}
	if ops.ZipFolder || ops.NoCompress || ops.GitIgnore || ops.Ask || ops.Quiet {
		t.Fatal("zip/no-compress/git/ask/quiet should default to false")
	}
	if ops.NoPrompt {
		t.Fatal("NoPrompt should default to false on send")
	}
	if len(ops.Exclude) != 0 {
		t.Fatalf("Exclude = %v, want empty", ops.Exclude)
	}
	if ops.ThrottleUpload != "" {
		t.Fatalf("ThrottleUpload = %q, want empty", ops.ThrottleUpload)
	}
}

func TestBuildSendOptionsAllFlags(t *testing.T) {
	exclude := []string{"node_modules", "*.tmp"}
	ops := buildSendOptions("test-code-123", "hello", true, true, true, true, true, true, exclude, "500k")

	if !ops.SendingText {
		t.Fatal("SendingText should be set with --text")
	}
	if !ops.ZipFolder {
		t.Fatal("ZipFolder should be set with --zip")
	}
	if !ops.NoCompress {
		t.Fatal("NoCompress should be set with --no-compress")
	}
	if !ops.GitIgnore {
		t.Fatal("GitIgnore should be set with --git")
	}
	if !ops.NoPrompt {
		t.Fatal("NoPrompt should be set with --yes")
	}
	if !ops.Ask {
		t.Fatal("Ask should be set with --ask")
	}
	if !ops.Quiet {
		t.Fatal("Quiet should be set with --quiet")
	}
	if !reflect.DeepEqual(ops.Exclude, exclude) {
		t.Fatalf("Exclude = %v, want %v", ops.Exclude, exclude)
	}
	if ops.ThrottleUpload != "500k" {
		t.Fatalf("ThrottleUpload = %q, want %q", ops.ThrottleUpload, "500k")
	}
}

func TestBuildRecvOptionsDefaults(t *testing.T) {
	ops := buildRecvOptions("test-code-123", 4, false, false, false)

	if ops.IsSender {
		t.Fatal("receiver options should not set IsSender")
	}
	if ops.SharedSecret != "test-code-123" {
		t.Fatalf("SharedSecret = %q, want %q", ops.SharedSecret, "test-code-123")
	}
	if !ops.NoPrompt {
		t.Fatal("recv should not prompt for acceptance by default")
	}
	if ops.Ask || ops.Quiet || ops.Stdout {
		t.Fatal("ask/quiet/stdout should default to false")
	}
	if len(ops.RelayPorts) != 5 {
		t.Fatalf("RelayPorts = %v, want 5 ports for 4 threads", ops.RelayPorts)
	}
	if ops.RelayAddress != "localhost:"+ops.RelayPorts[0] {
		t.Fatalf("RelayAddress = %q, want localhost relay", ops.RelayAddress)
	}
}

func TestBuildRecvOptionsAllFlags(t *testing.T) {
	ops := buildRecvOptions("test-code-123", 2, true, true, true)

	if !ops.Stdout {
		t.Fatal("Stdout should be set with --stdout")
	}
	if !ops.Ask {
		t.Fatal("Ask should be set with --ask")
	}
	if !ops.Quiet {
		t.Fatal("Quiet should be set with --quiet")
	}
	if !ops.NoPrompt {
		t.Fatal("NoPrompt should stay true even with --ask")
	}
	if len(ops.RelayPorts) != 3 {
		t.Fatalf("RelayPorts = %v, want 3 ports for 2 threads", ops.RelayPorts)
	}
}

func TestSendCommandHasNewFlags(t *testing.T) {
	cmd := newSendCmd(context.Background())
	args := []string{
		"--zip",
		"--no-compress",
		"--git",
		"--exclude", "a,b",
		"--throttleUpload", "500k",
		"--yes",
		"--ask",
		"--quiet",
		"--text", "hello",
	}
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatalf("ParseFlags() error = %v", err)
	}
	if v, _ := cmd.Flags().GetBool("zip"); !v {
		t.Fatal("zip flag should be enabled")
	}
	if v, _ := cmd.Flags().GetBool("no-compress"); !v {
		t.Fatal("no-compress flag should be enabled")
	}
	if v, _ := cmd.Flags().GetBool("git"); !v {
		t.Fatal("git flag should be enabled")
	}
	if v, _ := cmd.Flags().GetStringSlice("exclude"); len(v) != 2 {
		t.Fatalf("exclude = %v, want 2 entries", v)
	}
	if v, _ := cmd.Flags().GetString("throttleUpload"); v != "500k" {
		t.Fatalf("throttleUpload = %q, want %q", v, "500k")
	}
	if v, _ := cmd.Flags().GetBool("yes"); !v {
		t.Fatal("yes flag should be enabled")
	}
	if v, _ := cmd.Flags().GetBool("ask"); !v {
		t.Fatal("ask flag should be enabled")
	}
	if v, _ := cmd.Flags().GetBool("quiet"); !v {
		t.Fatal("quiet flag should be enabled")
	}
}

func TestRecvCommandHasNewFlags(t *testing.T) {
	cmd := newRecvCmd(context.Background())
	args := []string{
		"--stdout",
		"--yes",
		"--ask",
		"--quiet",
		"test-code-123",
	}
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatalf("ParseFlags() error = %v", err)
	}
	for _, name := range []string{"stdout", "yes", "ask", "quiet"} {
		if v, _ := cmd.Flags().GetBool(name); !v {
			t.Fatalf("%s flag should be enabled", name)
		}
	}
}
