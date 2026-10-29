package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os"

	"cercano/source/server/internal/acp"
	"cercano/source/server/internal/acp/v1"
	"cercano/source/server/pkg/agentclient"
	"cercano/source/server/pkg/config"
)

const acpProtocolVersion = v1.ProtocolVersion

type acpResponse = acp.Response

// detectACPStdio reports whether stdin looks like a live ACP peer, returning
// the buffered reader the session should go on reading from.
//
// Only pipes and sockets qualify. A character device is either a terminal (a
// human running bare `cercano` in a shell) or /dev/null (the daemon that
// `cercano-cli` auto-launches), and neither is an ACP client — peeking at a
// terminal would block until the user happened to type something. Regular
// files are excluded for the same reason: use the explicit `cercano acp`
// subcommand to replay a captured session from a file.
//
// For a pipe, one byte decides it. Immediate EOF means the far end is already
// closed, so there is no peer. Otherwise we have a real client, even if it
// took a moment to send `initialize`. Peek does not consume the byte, so the
// returned reader still carries the whole stream. A pipe that is open but
// never written to will block here, which is deliberate: a timeout would race
// against a slow editor, and a silent peer is indistinguishable from a
// not-yet-ready one.
func detectACPStdio() (*bufio.Reader, bool) {
	info, err := os.Stdin.Stat()
	if err != nil {
		return nil, false
	}
	if info.Mode()&(os.ModeNamedPipe|os.ModeSocket) == 0 {
		return nil, false
	}
	r := bufio.NewReader(os.Stdin)
	if _, err := r.Peek(1); err != nil {
		return nil, false
	}
	return r, true
}

// runACPMode connects to (or launches) the shared daemon, never an embedded server.
func runACPMode(cfg config.Config, in *bufio.Reader) {
	setupDispatchLogFile()
	fmt.Fprintf(os.Stderr, "Cercano ACP agent (v%s) on stdio\n", version)
	if in == nil {
		in = bufio.NewReader(os.Stdin)
	}
	input := struct {
		io.Reader
		io.Closer
	}{in, os.Stdin}
	output, err := acpOutput(os.Stdout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ACP output: %v\n", err)
		return
	}
	defer output.Close()
	err = acp.Serve(context.Background(), input, output, func(ctx context.Context) (acp.Backend, error) {
		client, err := agentclient.Dial(ctx, net.JoinHostPort("localhost", cfg.Port))
		if err != nil {
			return nil, err
		}
		return acp.NewAgentBackend(ctx, client), nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "ACP: %v\n", err)
	}
}
