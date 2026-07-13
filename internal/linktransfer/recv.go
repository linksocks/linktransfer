package linktransfer

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/linksocks/croc/src/croc"
	"github.com/linksocks/croc/src/models"
	"github.com/spf13/cobra"
)

func normalizeRecvError(code string, err error) error {
	if err == nil {
		return nil
	}

	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "socks connect tcp") ||
		strings.Contains(msg, "dial tcp 127.0.0.1:0") ||
		strings.Contains(msg, "could not connect to localhost:") ||
		strings.Contains(msg, "bad password") ||
		strings.Contains(msg, "message authentication failed") {
		return fmt.Errorf("unable to connect to sender. Check that the code is correct and the sender is still waiting. (code: %s)", code)
	}

	return err
}

func recvRetryMessage(err error) string {
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "sender disconnected") {
		return "Sender disconnected."
	}
	return "Sender is not available yet."
}

// waitPeerReady blocks until the tunnel reports at least one partner or ctx is cancelled.
// Returns true if a partner appeared, false if ctx was cancelled or timed out.
func waitPeerReady(ctx context.Context, trt *tunnelRuntime) bool {
	if trt == nil || trt.partnerCount == nil {
		return true
	}
	timeout := time.After(30 * time.Second)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-timeout:
			return false
		case <-ticker.C:
			if trt.partnerCount() > 0 {
				return true
			}
		}
	}
}

func watchDisconnect(ctx context.Context, trt *tunnelRuntime, cancel context.CancelFunc) <-chan error {
	if trt == nil || trt.partnerCount == nil {
		return nil
	}

	done := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		seenSender := trt.partnerCount() > 0
		var lostSince time.Time
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				count := trt.partnerCount()
				if count > 0 {
					seenSender = true
					lostSince = time.Time{}
					continue
				}
				if seenSender {
					if lostSince.IsZero() {
						lostSince = time.Now()
						continue
					}
					if time.Since(lostSince) < 2*time.Second {
						continue
					}
					cancel()
					done <- fmt.Errorf("sender disconnected")
					return
				}
			}
		}
	}()
	return done
}

func newRecvCmd(ctx context.Context) *cobra.Command {
	var out string
	var code string
	var tunnel tunnelOptions

	cmd := &cobra.Command{
		Use:   "recv [code]",
		Short: "Receive files",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if tunnel.Threads < 1 {
				return fmt.Errorf("--threads must be at least 1")
			}
			if len(args) == 1 {
				code = strings.TrimSpace(args[0])
			}
			if code == "" {
				code = strings.TrimSpace(os.Getenv("CROC_SECRET"))
			}
			if code == "" {
				return fmt.Errorf("missing code (provide as argument or set CROC_SECRET)")
			}

			if tunnel.Token == "" {
				tunnel.Token = tokenFromCode(code)
			}

			if tunnel.URL == defaultWSURL {
				fmt.Fprintf(os.Stderr, "Connecting to public relay server ...\n")
			} else {
				fmt.Fprintf(os.Stderr, "Connecting to %s ...\n", tunnel.URL)
			}
			trt, err := startReceiverTunnel(ctx, tunnel)
			if err != nil {
				return err
			}
			defer trt.close()
			fmt.Fprintf(os.Stderr, "Tunnel ready, connecting to sender...\n")

			ops := croc.Options{IsSender: false}
			applyCommonCrocOptions(&ops)
			ops.SharedSecret = code
			ops.RelayPassword = models.DEFAULT_PASSPHRASE
			ops.RelayPorts = relayPortsFromCode(code, tunnel.Threads)
			ops.RelayAddress = "localhost:" + ops.RelayPorts[0]
			ops.NoPrompt = true
			ops.SilenceInstructions = true

			if out != "" {
				if err := os.Chdir(out); err != nil {
					return err
				}
			}

			const maxRetries = 3
			var recvErr error
			for attempt := 0; attempt <= maxRetries; attempt++ {
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
				}

				// Wait for sender to appear on the tunnel before attempting (except first try)
				if attempt > 0 {
					fmt.Fprintf(os.Stderr, "\nWaiting for sender to be ready...\n")
					if ctx.Err() != nil {
						return ctx.Err()
					}
					if !waitPeerReady(ctx, trt) {
						if ctx.Err() != nil {
							return ctx.Err()
						}
						// Timeout waiting for sender - continue to retry anyway
						fmt.Fprintf(os.Stderr, "Sender not detected, attempting reconnect...\n")
					}
				}

				attemptCtx, cancelAttempt := context.WithCancel(ctx)
				client, err := croc.NewCtx(attemptCtx, ops)
				if err != nil {
					cancelAttempt()
					return normalizeRecvError(code, err)
				}

				done := make(chan error, 1)
				go func() {
					done <- client.Receive()
				}()
				senderDisconnect := watchDisconnect(attemptCtx, trt, cancelAttempt)

				select {
				case recvErr = <-done:
				case recvErr = <-senderDisconnect:
					select {
					case <-done:
					case <-time.After(time.Second):
					}
				case <-ctx.Done():
					cancelAttempt()
					return ctx.Err()
				}
				cancelAttempt()

				if recvErr == nil {
					return nil
				}

				if ctx.Err() != nil {
					return ctx.Err()
				}

				if attempt < maxRetries {
					base := time.Duration(attempt+1) * time.Second
					jitter := time.Duration(rand.Intn(500)) * time.Millisecond
					delay := base + jitter
					fmt.Fprintf(os.Stderr, "\n%s Retrying in %s (%d/%d)...\n", recvRetryMessage(recvErr), delay.Round(time.Millisecond), attempt+1, maxRetries)
					select {
					case <-ctx.Done():
						return ctx.Err()
					case <-time.After(delay):
					}
				}
			}

			fmt.Fprintln(os.Stderr)
			return fmt.Errorf("receive failed after %d attempts: %w", maxRetries+1, normalizeRecvError(code, recvErr))
		},
	}

	cmd.Flags().StringVar(&out, "out", ".", "Output folder")
	cmd.Flags().StringVarP(&code, "code", "c", "", "Code phrase")
	addTunnelFlags(cmd, &tunnel)

	return cmd
}
