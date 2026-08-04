package linktransfer

import (
	"context"
	"errors"
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

func isPeerGoneError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "disconnected") ||
		strings.Contains(msg, "peer left") ||
		strings.Contains(msg, "sender is gone") ||
		strings.Contains(msg, "receiver is gone")
}

func recvRetryMessage(err error) string {
	if isPeerGoneError(err) {
		return "Sender left the transfer."
	}
	return "Sender is not available yet."
}

// waitPeerReady blocks until the tunnel reports at least one partner or ctx is cancelled.
// Returns true if a partner appeared, false if ctx was cancelled or timed out.
func waitPeerReady(ctx context.Context, trt *tunnelRuntime, timeout time.Duration) bool {
	if trt == nil || trt.partnerCount == nil {
		return true
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	deadline := time.After(timeout)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-deadline:
			return false
		case <-ticker.C:
			if trt.partnerCount() > 0 {
				return true
			}
		}
	}
}

// watchPeerDisconnect cancels the attempt when a previously-seen tunnel partner
// disappears, or when this side's WebSocket drops after a partner was present.
// peerLabel is used only in the returned error ("sender" / "receiver").
func watchPeerDisconnect(ctx context.Context, trt *tunnelRuntime, cancel context.CancelFunc, peerLabel string) <-chan error {
	if trt == nil || trt.partnerCount == nil {
		return nil
	}
	if peerLabel == "" {
		peerLabel = "peer"
	}

	done := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		seenPartner := trt.partnerCount() > 0
		var lostSince time.Time

		var discCh <-chan struct{}
		if trt.disconnected != nil {
			discCh = trt.disconnected()
		}

		for {
			select {
			case <-ctx.Done():
				return
			case <-discCh:
				if seenPartner {
					cancel()
					done <- fmt.Errorf("%s is gone (tunnel closed)", peerLabel)
					return
				}
				if trt.disconnected != nil {
					discCh = trt.disconnected()
				}
			case <-ticker.C:
				count := trt.partnerCount()
				if count > 0 {
					seenPartner = true
					lostSince = time.Time{}
					continue
				}
				if seenPartner {
					if lostSince.IsZero() {
						lostSince = time.Now()
						continue
					}
					if time.Since(lostSince) < 2*time.Second {
						continue
					}
					cancel()
					done <- fmt.Errorf("%s is gone", peerLabel)
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

			// Connection setup may need a few tries; mid-transfer peer loss should not.
			const maxConnectRetries = 3
			var recvErr error
			connectedOnce := false

			for attempt := 0; attempt <= maxConnectRetries; attempt++ {
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
				}

				if attempt > 0 {
					if connectedOnce || isPeerGoneError(recvErr) {
						// Sender vanished after we had a live session: stop quickly.
						break
					}
					fmt.Fprintf(os.Stderr, "\nWaiting for sender to be ready...\n")
					if ctx.Err() != nil {
						return ctx.Err()
					}
					if !waitPeerReady(ctx, trt, 15*time.Second) {
						if ctx.Err() != nil {
							return ctx.Err()
						}
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
				senderDisconnect := watchPeerDisconnect(attemptCtx, trt, cancelAttempt, "sender")

				select {
				case recvErr = <-done:
				case recvErr = <-senderDisconnect:
					select {
					case recvResult := <-done:
						// The peer vanished at the same moment the transfer finished.
						// Trust the actual transfer result so a successful completion
						// is not misreported as a peer loss.
						recvErr = recvResult
					case <-time.After(time.Second):
					}
				case <-ctx.Done():
					cancelAttempt()
					return ctx.Err()
				}
				cancelAttempt()

				if recvErr == nil {
					fmt.Fprintln(os.Stderr, "Transfer complete.")
					return nil
				}

				if ctx.Err() != nil {
					return ctx.Err()
				}

				// Any successful tunnel pairing or progress attempt counts as "had a session".
				if trt.partnerCount != nil && trt.partnerCount() > 0 {
					connectedOnce = true
				}
				if isPeerGoneError(recvErr) {
					connectedOnce = true
					fmt.Fprintln(os.Stderr, "\nSender left. Incomplete files may remain in the output folder.")
					break
				}

				if attempt < maxConnectRetries {
					base := time.Duration(attempt+1) * time.Second
					jitter := time.Duration(rand.Intn(500)) * time.Millisecond
					delay := base + jitter
					fmt.Fprintf(os.Stderr, "\n%s Retrying in %s (%d/%d)...\n", recvRetryMessage(recvErr), delay.Round(time.Millisecond), attempt+1, maxConnectRetries)
					select {
					case <-ctx.Done():
						return ctx.Err()
					case <-time.After(delay):
					}
				}
			}

			fmt.Fprintln(os.Stderr)
			if isPeerGoneError(recvErr) {
				return fmt.Errorf("transfer interrupted: sender left (code: %s)", code)
			}
			if recvErr == nil {
				recvErr = errors.New("receive failed")
			}
			return fmt.Errorf("receive failed after %d attempts: %w", maxConnectRetries+1, normalizeRecvError(code, recvErr))
		},
	}

	cmd.Flags().StringVar(&out, "out", ".", "Output folder")
	cmd.Flags().StringVarP(&code, "code", "c", "", "Code phrase")
	addTunnelFlags(cmd, &tunnel)

	return cmd
}
