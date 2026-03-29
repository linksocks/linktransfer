package linktransfer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

func Main() {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	exitCodeFor := func(sig os.Signal) int {
		if s, ok := sig.(syscall.Signal); ok {
			return 128 + int(s)
		}
		return 130
	}

	go func() {
		sig := <-sigCh
		cancel()

		go func() {
			sig2 := <-sigCh
			os.Exit(exitCodeFor(sig2))
		}()

		select {
		case <-done:
			return
		case <-time.After(5 * time.Second):
			os.Exit(exitCodeFor(sig))
		}
	}()

	root := newRootCmd(ctx)
	root.SetContext(ctx)
	err := root.Execute()
	close(done)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			os.Exit(130)
		}
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

func newRootCmd(ctx context.Context) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "lt",
		Short:         "Transfer files between computers via linksocks tunnels",
		Version:       Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.AddCommand(newSendCmd(ctx))
	cmd.AddCommand(newRecvCmd(ctx))

	return cmd
}
