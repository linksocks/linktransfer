package linktransfer

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/linksocks/croc/src/croc"
	"github.com/linksocks/croc/src/models"
	"github.com/spf13/cobra"
)

func newSendCmd(ctx context.Context) *cobra.Command {
	var code string
	var text string
	var tunnel tunnelOptions

	cmd := &cobra.Command{
		Use:   "send [file-or-dir]...",
		Short: "Send files or folders",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && text == "" {
				return fmt.Errorf("must specify at least one path, or use --text")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if tunnel.Threads < 1 {
				return fmt.Errorf("--threads must be at least 1")
			}
			if code == "" {
				code = getRandomCode()
			}
			if tunnel.Token == "" {
				tunnel.Token = tokenFromCode(code)
			}

			fnames := make([]string, 0, len(args)+1)
			for _, a := range args {
				fnames = append(fnames, filepath.Clean(a))
			}
			if text != "" {
				fname, err := writeTempTextFile(text)
				if err != nil {
					return err
				}
				defer os.Remove(fname)
				fnames = append(fnames, fname)
			}

			ops := croc.Options{IsSender: true}
			applyCommonCrocOptions(&ops)
			ops.SharedSecret = code
			ops.SendingText = text != ""
			ops.RelayPassword = models.DEFAULT_PASSPHRASE
			ops.DisableClipboard = true
			ops.SilenceInstructions = true

			filesInfo, emptyFolders, totalFolders, err := croc.GetFilesInfo(fnames, ops.ZipFolder, ops.GitIgnore, ops.Exclude)
			if err != nil {
				return err
			}

			basePort := relayBasePortFromCode(code, tunnel.Threads)
			relayPorts, err := setupLocalRelay(basePort, tunnel.Threads+1, ops.RelayPassword)
			if err != nil {
				return fmt.Errorf("failed to start local relay: %w", err)
			}
			ops.RelayPorts = relayPorts
			ops.RelayAddress = "127.0.0.1:" + relayPorts[0]

			if tunnel.URL == defaultWSURL {
				fmt.Fprintf(os.Stderr, "Connecting to public relay server ...\n")
			} else {
				fmt.Fprintf(os.Stderr, "Connecting to %s ...\n", tunnel.URL)
			}
			trt, err := startSenderTunnel(ctx, tunnel)
			if err != nil {
				return err
			}
			defer trt.close()

			fmt.Fprintf(os.Stderr, "\nOn the other computer run:\n  lt recv %s\n\n", code)

			// Keep the sender available after a live receiver drops.
			const maxRetries = 3
			var sendErr error
			connectedOnce := false
			attempt := 0
			for {
				if attempt > 0 {
					if connectedOnce {
						fmt.Fprintf(os.Stderr, "\nReceiver left. Waiting for a new receiver with the same code...\n  lt recv %s\n\n", code)
					} else {
						if attempt > maxRetries {
							break
						}
						fmt.Fprintf(os.Stderr, "\nTransfer failed (%v). Retrying with the same code (%d/%d)...\n  lt recv %s\n\n", sendErr, attempt, maxRetries, code)
					}
				}

				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
				}

				attemptCtx, cancelAttempt := context.WithCancel(ctx)
				client, err := croc.NewCtx(attemptCtx, ops)
				if err != nil {
					cancelAttempt()
					return err
				}

				done := make(chan error, 1)
				go func() {
					done <- client.Send(filesInfo, emptyFolders, totalFolders)
				}()

				receiverDisconnect := watchPeerDisconnect(attemptCtx, trt, cancelAttempt, "receiver")

				select {
				case sendErr = <-done:
				case sendErr = <-receiverDisconnect:
					select {
					case sendResult := <-done:
						// The peer vanished at the same moment the transfer finished.
						// Trust the actual transfer result so a successful completion
						// is not misreported as a peer loss.
						sendErr = transferResultAfterPeerDisconnect(sendErr, sendResult)
					case <-time.After(time.Second):
					}
				case <-ctx.Done():
					cancelAttempt()
					return ctx.Err()
				}
				cancelAttempt()

				if sendErr == nil {
					fmt.Fprintln(os.Stderr, "Transfer complete.")
					return nil
				}

				if ctx.Err() != nil {
					return ctx.Err()
				}

				partnerCount := 0
				if trt.partnerCount != nil {
					partnerCount = trt.partnerCount()
				}
				connectedOnce = transferSessionActive(connectedOnce, partnerCount, sendErr)
				if !connectedOnce && attempt >= maxRetries {
					break
				}

				// Peer-gone: short pause then re-open wait. Other errors: backoff.
				delay := 500 * time.Millisecond
				if !connectedOnce {
					base := time.Duration(attempt+1) * time.Second
					jitter := time.Duration(rand.Intn(500)) * time.Millisecond
					delay = base + jitter
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(delay):
				}
				attempt++
			}

			if isPeerGoneError(sendErr) {
				return fmt.Errorf("no receiver reconnected after %d attempts (code: %s)", maxRetries+1, code)
			}
			if sendErr != nil && strings.TrimSpace(sendErr.Error()) == "" {
				return fmt.Errorf("send failed")
			}
			return sendErr
		},
	}

	cmd.Flags().StringVarP(&code, "code", "c", "", "Code phrase (random if omitted)")
	cmd.Flags().StringVar(&text, "text", "", "Send text instead of files")
	addTunnelFlags(cmd, &tunnel)

	return cmd
}
