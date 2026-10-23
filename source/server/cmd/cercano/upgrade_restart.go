package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"time"

	"cercano/source/server/internal/brewrestart"
	"cercano/source/server/internal/modelhost"
	"cercano/source/server/pkg/config"
	"cercano/source/server/pkg/proto"
)

func runUpgradeRestart(args []string) int {
	return upgradeRestartCommand(args, os.Stdout,
		func() (string, error) { cfg, err := config.Load(config.DefaultPath()); return cfg.Port, err },
		func() (string, error) {
			path, err := os.Executable()
			if err != nil {
				return "", err
			}
			return filepath.EvalSymlinks(path)
		},
		shutdownModelHost,
		brewrestart.RestartInstalled)
}

// shutdownModelHost asks the shared model host to exit so the next model
// request launches the installed binary.
func shutdownModelHost(ctx context.Context) error {
	dir, err := modelhost.RunDir()
	if err != nil {
		return err
	}
	return shutdownModelHostIn(dir)(ctx)
}

// shutdownModelHostIn returns a shutdown function for the host recorded in
// dir. A missing record, or a recorded host that no longer answers, means
// there is nothing to shut down and is not an error.
func shutdownModelHostIn(dir string) func(context.Context) error {
	return func(ctx context.Context) error {
		rec, err := modelhost.ReadRecord(dir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		conn, err := modelhost.Dial(ctx, rec)
		if err != nil {
			return nil
		}
		defer conn.Close()
		_, err = proto.NewModelHostClient(conn).Shutdown(ctx, &proto.ShutdownRequest{})
		return err
	}
}

func upgradeRestartCommand(args []string, out io.Writer, configuredPort func() (string, error), executable func() (string, error), shutdownModelHost func(context.Context) error, restart func(context.Context, string, netip.AddrPort) (bool, error)) int {
	flags := flag.NewFlagSet("restart-after-upgrade", flag.ContinueOnError)
	flags.SetOutput(out)
	address := flags.String("address", "", "explicit loopback agent address (default: configured port)")
	timeout := flags.Duration("timeout", 2*time.Minute, "deadline for drain and restart (maximum 10m)")
	flags.Usage = func() {
		fmt.Fprintln(out, "Usage: cercano restart-after-upgrade [--address 127.0.0.1:PORT] [--timeout 2m]\nRestart an existing agent owned by this Homebrew installation after a successful upgrade.\nDoes not start an absent agent, install updates, or compare versions.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 || *timeout <= 0 || *timeout > 10*time.Minute {
		fmt.Fprintln(out, "Invalid arguments or restart deadline.")
		return 2
	}
	if *address == "" {
		port, err := configuredPort()
		if err != nil {
			fmt.Fprintln(out, "Cannot load agent configuration; no restart attempted.")
			return 1
		}
		*address = "127.0.0.1:" + port
	}
	endpoint, err := netip.ParseAddrPort(*address)
	if err != nil || !endpoint.Addr().IsLoopback() || endpoint.Port() == 0 || endpoint.Addr().Zone() != "" {
		fmt.Fprintln(out, "Restart requires a numeric loopback address and nonzero port.")
		return 2
	}
	path, err := executable()
	if err != nil {
		fmt.Fprintln(out, "Cannot resolve installed executable; no restart attempted.")
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	restarted, err := restart(ctx, path, endpoint)
	if err != nil {
		fmt.Fprintf(out, "Upgrade restart failed: %v\nThe update may already be installed. Check agent logs; use /restart-agent if the old agent is still serving, or start cercano manually if no agent is running.\n", err)
		return 1
	}
	// The model host is shut down even when no agent was running, so the next
	// request launches the new binary. A failure here is reported but does not
	// change the exit code: the agent restart itself succeeded.
	if err := shutdownModelHost(ctx); err != nil {
		fmt.Fprintf(out, "Could not shut down the shared model host: %v\nIt will exit when idle; the next model request then launches the installed binary.\n", err)
	}
	if restarted {
		fmt.Fprintln(out, "Running agent restarted using the installed binary.")
	} else {
		fmt.Fprintln(out, "No running agent owned by this installation; nothing started.")
	}
	return 0
}
