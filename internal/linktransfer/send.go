package linktransfer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/schollz/croc/v10/src/croc"
	"github.com/schollz/croc/v10/src/models"
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

			filesInfo, emptyFolders, totalFolders, err := croc.GetFilesInfo(fnames, ops.ZipFolder, ops.GitIgnore, ops.Exclude)
			if err != nil {
				return err
			}

			basePort := relayBasePortFromCode(code)
			relayPorts, err := setupLocalRelay(basePort, relayPortCount, ops.RelayPassword)
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

			client, err := croc.New(ops)
			if err != nil {
				return err
			}

			sf := newStderrFilter()
			sf.suppress(true)

			done := make(chan error, 1)
			go func() {
				done <- client.Send(filesInfo, emptyFolders, totalFolders)
			}()

			var sendErr error
			select {
			case sendErr = <-done:
			case <-ctx.Done():
				sendErr = ctx.Err()
			}

			sf.restore()
			return sendErr
		},
	}

	cmd.Flags().StringVarP(&code, "code", "c", "", "Code phrase (random if omitted)")
	cmd.Flags().StringVar(&text, "text", "", "Send text instead of files")
	addTunnelFlags(cmd, &tunnel)

	return cmd
}
