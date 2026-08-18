package linktransfer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
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
	if errors.Is(err, net.ErrClosed) || errors.Is(err, io.EOF) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "disconnected") ||
		strings.Contains(msg, "peer left") ||
		strings.Contains(msg, "sender is gone") ||
		strings.Contains(msg, "receiver is gone") ||
		strings.Contains(msg, "use of closed network connection") ||
		strings.Contains(msg, "connection reset by peer") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "unexpected eof")
}

func transferSessionActive(active bool, partnerCount int, err error) bool {
	return active || partnerCount > 0 || isPeerGoneError(err)
}

func transferResultAfterPeerDisconnect(peerErr, transferErr error) error {
	if transferErr == nil {
		return nil
	}
	if errors.Is(transferErr, context.Canceled) {
		return peerErr
	}
	return transferErr
}

const peerDisconnectGracePeriod = 3 * time.Second

func waitForTransferAfterPeerDisconnect(done <-chan error, peerErr error, cancel context.CancelFunc) error {
	select {
	case transferErr := <-done:
		return transferResultAfterPeerDisconnect(peerErr, transferErr)
	case <-time.After(peerDisconnectGracePeriod):
		cancel()
		select {
		case transferErr := <-done:
			return transferResultAfterPeerDisconnect(peerErr, transferErr)
		case <-time.After(time.Second):
			return peerErr
		}
	}
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

// watchPeerDisconnect reports when a previously-seen tunnel partner disappears.
// peerLabel is used only in the returned error ("sender" / "receiver").
func watchPeerDisconnect(ctx context.Context, trt *tunnelRuntime, peerLabel string) <-chan error {
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
		currentDisconnect := func() <-chan struct{} {
			if trt.disconnected == nil {
				return nil
			}
			ch := trt.disconnected()
			if ch == nil {
				return nil
			}
			select {
			case <-ch:
				return nil
			default:
				return ch
			}
		}

		seenPartner := trt.partnerCount() > 0
		var lostSince time.Time
		discCh := currentDisconnect()

		for {
			select {
			case <-ctx.Done():
				return
			case <-discCh:
				if seenPartner {
					done <- fmt.Errorf("%s is gone (tunnel closed)", peerLabel)
					return
				}
				discCh = nil
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
					done <- fmt.Errorf("%s is gone", peerLabel)
					return
				}
				if discCh == nil {
					discCh = currentDisconnect()
				}
			}
		}
	}()
	return done
}

func newRecvCmd(ctx context.Context) *cobra.Command {
	var out string
	var code string
	var overwrite bool
	var stdout bool
	var yes bool
	var ask bool
	var quiet bool
	var tunnel tunnelOptions

	cmd := &cobra.Command{
		Use:   "recv [code]",
		Short: "Receive files or folders from another computer",
		Long:  "Receive files or folders from another computer using a transfer code.\n\nUse --out to choose the destination and --overwrite to skip existing-file prompts.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			status := func(format string, args ...any) {
				if !quiet {
					fmt.Fprintf(os.Stderr, format, args...)
				}
			}
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

			if tunnel.URL == defaultWSURL {
				status("Connecting to public relay server ...\n")
			} else {
				status("Connecting to %s ...\n", tunnel.URL)
			}
			trt, err := startReceiverTunnel(ctx, tunnel, code, tokenFromCode(code))
			if err != nil {
				return err
			}
			defer trt.close()
			status("Tunnel ready, connecting to sender...\n")

			ops := buildRecvOptions(code, tunnel.Threads, stdout, ask, quiet)

			if out != "" {
				if err := os.Chdir(out); err != nil {
					return err
				}
			}

			// Connection setup has a bounded retry budget; an interrupted session waits for reconnection.
			const maxConnectRetries = 3
			var recvErr error
			connectedOnce := false
			waitingForSender := false
			attempt := 0

			for {
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
				}

				if attempt > 0 {
					if connectedOnce {
						if !waitingForSender {
							status("\nWaiting for sender to reconnect...\n")
							waitingForSender = true
						}
					} else {
						if attempt > maxConnectRetries {
							break
						}
						status("\nWaiting for sender to be ready...\n")
					}
					if ctx.Err() != nil {
						return ctx.Err()
					}
					if !waitPeerReady(ctx, trt, 15*time.Second) {
						if ctx.Err() != nil {
							return ctx.Err()
						}
						status("Sender not detected, attempting reconnect...\n")
					}
				}

				attemptCtx, cancelAttempt := context.WithCancel(ctx)
				ops.Overwrite = overwrite || attempt > 0
				client, err := croc.NewCtx(attemptCtx, ops)
				if err != nil {
					cancelAttempt()
					return normalizeRecvError(code, err)
				}

				done := make(chan error, 1)
				go func() {
					done <- client.Receive()
				}()
				senderDisconnect := watchPeerDisconnect(attemptCtx, trt, "sender")

				select {
				case recvErr = <-done:
				case peerErr := <-senderDisconnect:
					recvErr = waitForTransferAfterPeerDisconnect(done, peerErr, cancelAttempt)
				case <-ctx.Done():
					cancelAttempt()
					return ctx.Err()
				}
				cancelAttempt()

				if recvErr == nil {
					status("Transfer complete.\n")
					return nil
				}

				if ctx.Err() != nil {
					return ctx.Err()
				}

				partnerCount := 0
				if trt.partnerCount != nil {
					partnerCount = trt.partnerCount()
				}
				connectedOnce = transferSessionActive(connectedOnce, partnerCount, recvErr)

				if !connectedOnce && attempt >= maxConnectRetries {
					break
				}

				backoffAttempt := attempt + 1
				if connectedOnce {
					backoffAttempt = 1
				}
				base := time.Duration(backoffAttempt) * time.Second
				jitter := time.Duration(rand.Intn(500)) * time.Millisecond
				delay := base + jitter
				if connectedOnce {
					if !waitingForSender {
						status("\n%s\n", recvRetryMessage(recvErr))
						waitingForSender = true
					}
				} else {
					status("\n%s Retrying in %s (%d/%d)...\n", recvRetryMessage(recvErr), delay.Round(time.Millisecond), attempt+1, maxConnectRetries)
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(delay):
				}
				attempt++
			}

			status("\n")
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
	cmd.Flags().BoolVar(&overwrite, "overwrite", false, "Overwrite existing files without prompting")
	cmd.Flags().BoolVar(&stdout, "stdout", false, "Write received content to stdout instead of files")
	cmd.Flags().BoolVar(&yes, "yes", false, "Automatically agree to all prompts")
	cmd.Flags().BoolVar(&ask, "ask", false, "Confirm the sender machine before receiving")
	cmd.Flags().BoolVar(&quiet, "quiet", false, "Disable all output")
	addTunnelFlags(cmd, &tunnel)

	return cmd
}

func buildRecvOptions(code string, threads int, stdout, ask, quiet bool) croc.Options {
	ops := croc.Options{IsSender: false}
	applyCommonCrocOptions(&ops)
	ops.SharedSecret = code
	ops.RelayPassword = models.DEFAULT_PASSPHRASE
	ops.RelayPorts = relayPortsFromCode(code, threads)
	ops.RelayAddress = "localhost:" + ops.RelayPorts[0]
	ops.NoPrompt = true
	ops.Ask = ask
	ops.Quiet = quiet
	ops.Stdout = stdout
	ops.SilenceInstructions = true
	return ops
}
