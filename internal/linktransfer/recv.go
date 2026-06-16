package linktransfer

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/linksocks/croc/src/croc"
	"github.com/linksocks/croc/src/models"
	log "github.com/schollz/logger"
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
		return fmt.Errorf("invalid code or sender is unavailable (code: %s)", code)
	}

	return err
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
			ops.RelayPorts = relayPortsFromCode(code)
			ops.RelayAddress = "localhost:" + ops.RelayPorts[0]
			ops.NoPrompt = true

			if out != "" {
				if err := os.Chdir(out); err != nil {
					return err
				}
			}

			const maxRetries = 3
			var recvErr error
			for attempt := 0; attempt <= maxRetries; attempt++ {
				if attempt > 0 {
					log.Warnf("[retry %d/%d] reconnecting with same code: %s", attempt, maxRetries, code)
				}

				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
				}

				client, err := croc.NewCtx(ctx, ops)
				if err != nil {
					return normalizeRecvError(code, err)
				}

				done := make(chan error, 1)
				go func() {
					done <- client.Receive()
				}()

				select {
				case recvErr = <-done:
				case <-ctx.Done():
					return ctx.Err()
				}

				if recvErr == nil {
					return nil
				}

				if ctx.Err() != nil {
					return ctx.Err()
				}

				log.Warnf("[error] receive failed: %v", normalizeRecvError(code, recvErr))
				if attempt < maxRetries {
					time.Sleep(time.Duration(attempt+1) * time.Second)
				}
			}
			return normalizeRecvError(code, recvErr)
		},
	}

	cmd.Flags().StringVar(&out, "out", ".", "Output folder")
	cmd.Flags().StringVarP(&code, "code", "c", "", "Code phrase")
	addTunnelFlags(cmd, &tunnel)

	return cmd
}
