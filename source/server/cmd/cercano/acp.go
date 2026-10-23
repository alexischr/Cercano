package main

// ACP (Agent Client Protocol) stdio mode.
//
// ACP is the JSON-RPC protocol editors like Zed use to drive a coding agent:
// the editor is the *client*, this binary is the *agent*. Messages are
// newline-delimited JSON objects on stdin/stdout, which is why every
// human-readable line this binary emits must go to stderr — see runServerMode.
//
// This file currently implements the transport and the `initialize` handshake
// only. Session and prompt methods answer with a JSON-RPC "method not found"
// so a connecting editor gets a clean protocol error instead of a hang.

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"cercano/source/server/pkg/config"
)

// acpProtocolVersion is the ACP wire version this agent speaks. Compatibility
// is negotiated on this number during `initialize`, not on any library version.
const acpProtocolVersion = 1

type acpRequest struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method"`
	Params  json.RawMessage  `json:"params,omitempty"`
}

type acpResponse struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Result  any              `json:"result,omitempty"`
	Error   *acpError        `json:"error,omitempty"`
}

type acpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

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

// runACPMode serves the ACP agent side over stdio until the client hangs up.
// in may be nil, in which case stdin is read directly — that is the path for
// the explicit `cercano acp` subcommand, where no detection peek happened.
func runACPMode(cfg config.Config, in *bufio.Reader) {
	// Tee diagnostics to a file: an editor's capture of our stderr may close
	// early, and stdout is unavailable for anything but protocol frames.
	setupDispatchLogFile()
	fmt.Fprintf(os.Stderr, "Cercano ACP agent (v%s) on stdio\n", version)

	if in == nil {
		in = bufio.NewReader(os.Stdin)
	}
	dec := json.NewDecoder(in)
	out := json.NewEncoder(os.Stdout)

	for {
		var req acpRequest
		if err := dec.Decode(&req); err != nil {
			if errors.Is(err, io.EOF) {
				fmt.Fprintln(os.Stderr, "ACP client disconnected")
				return
			}
			fmt.Fprintf(os.Stderr, "ACP decode error: %v\n", err)
			return
		}

		resp := acpResponse{JSONRPC: "2.0", ID: req.ID}
		switch req.Method {
		case "initialize":
			resp.Result = map[string]any{
				"protocolVersion": acpProtocolVersion,
				"agentCapabilities": map[string]any{
					"loadSession": false,
				},
			}
		default:
			resp.Error = &acpError{
				Code:    -32601,
				Message: fmt.Sprintf("method not implemented: %s", req.Method),
			}
		}

		// A request without an id is a notification: no reply allowed.
		if req.ID == nil {
			continue
		}
		if err := out.Encode(resp); err != nil {
			fmt.Fprintf(os.Stderr, "ACP write error: %v\n", err)
			return
		}
	}
}
