package main

import (
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"cercano/source/server/pkg/config"
)

// withStdin swaps os.Stdin for the duration of a test.
func withStdin(t *testing.T, f *os.File) {
	t.Helper()
	orig := os.Stdin
	os.Stdin = f
	t.Cleanup(func() { os.Stdin = orig })
}

func TestDetectACPStdio_CharDeviceIsNotACP(t *testing.T) {
	// /dev/null is what pkg/agentclient hands the auto-launched daemon, and a
	// TTY is what a human gets in a shell. Both are character devices and both
	// must fall through to server mode without blocking.
	devnull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("open %s: %v", os.DevNull, err)
	}
	defer devnull.Close()
	withStdin(t, devnull)

	done := make(chan bool, 1)
	go func() {
		_, ok := detectACPStdio()
		done <- ok
	}()
	select {
	case ok := <-done:
		if ok {
			t.Fatal("char device classified as ACP; the daemon would never serve gRPC")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("detectACPStdio blocked on a char device")
	}
}

func TestDetectACPStdio_ClosedPipeIsNotACP(t *testing.T) {
	// A pipe whose writer is already gone yields immediate EOF: no peer.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer r.Close()
	w.Close()
	withStdin(t, r)

	if _, ok := detectACPStdio(); ok {
		t.Fatal("closed pipe classified as ACP")
	}
}

func TestDetectACPStdio_PipeWithDataIsACP(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer r.Close()
	withStdin(t, r)

	payload := `{"jsonrpc":"2.0","id":1,"method":"initialize"}` + "\n"
	go func() {
		_, _ = w.WriteString(payload)
		w.Close()
	}()

	buf, ok := detectACPStdio()
	if !ok {
		t.Fatal("pipe with data not classified as ACP")
	}
	// The peeked byte must still be there, or the handshake is corrupt.
	got, err := io.ReadAll(buf)
	if err != nil {
		t.Fatalf("read after peek: %v", err)
	}
	if string(got) != payload {
		t.Fatalf("peek consumed input:\n got %q\nwant %q", got, payload)
	}
}

func TestDetectACPStdio_SlowPipeIsACP(t *testing.T) {
	// An editor that spawns us and takes a beat before sending `initialize`
	// is a legitimate client, not a dead peer.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer r.Close()
	withStdin(t, r)

	go func() {
		time.Sleep(300 * time.Millisecond)
		_, _ = w.WriteString("{}\n")
		w.Close()
	}()

	start := time.Now()
	if _, ok := detectACPStdio(); !ok {
		t.Fatal("slow pipe not classified as ACP")
	}
	if elapsed := time.Since(start); elapsed < 250*time.Millisecond {
		t.Fatalf("returned in %v, before data could arrive", elapsed)
	}
}

func TestDetectACPStdio_SocketIsACP(t *testing.T) {
	// Editors may hand us a socketpair rather than a pipe.
	dir := t.TempDir()
	addr := filepath.Join(dir, "s.sock")
	ln, err := net.Listen("unix", addr)
	if err != nil {
		t.Skipf("unix socket unavailable: %v", err)
	}
	defer ln.Close()

	go func() {
		c, err := net.Dial("unix", addr)
		if err != nil {
			return
		}
		_, _ = c.Write([]byte("{}\n"))
	}()

	conn, err := ln.Accept()
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	defer conn.Close()
	f, err := conn.(*net.UnixConn).File()
	if err != nil {
		t.Fatalf("socket to file: %v", err)
	}
	defer f.Close()
	withStdin(t, f)

	if _, ok := detectACPStdio(); !ok {
		t.Fatal("socket with data not classified as ACP")
	}
}

func TestDetectACPStdio_RegularFileIsNotACP(t *testing.T) {
	// Redirecting a file into bare `cercano` should still start the server;
	// replaying a captured session is what `cercano acp` is for.
	path := filepath.Join(t.TempDir(), "in.json")
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	withStdin(t, f)

	if _, ok := detectACPStdio(); ok {
		t.Fatal("regular file classified as ACP")
	}
}

// TestACPInitializeHandshake drives runACPMode's request loop over a pipe pair
// and checks the negotiated protocol version comes back on stdout.
func TestACPInitializeHandshake(t *testing.T) {
	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}

	origOut := os.Stdout
	os.Stdout = outW
	t.Cleanup(func() { os.Stdout = origOut })
	withStdin(t, inR)

	go func() {
		_, _ = inW.WriteString(`{"jsonrpc":"2.0","id":7,"method":"initialize","params":{"protocolVersion":1}}` + "\n")
		_, _ = inW.WriteString(`{"jsonrpc":"2.0","id":8,"method":"session/prompt"}` + "\n")
		inW.Close()
	}()

	done := make(chan struct{})
	go func() {
		runACPMode(config.Defaults(), nil)
		outW.Close()
		close(done)
	}()

	dec := json.NewDecoder(outR)

	var initResp acpResponse
	if err := dec.Decode(&initResp); err != nil {
		t.Fatalf("decode initialize response: %v", err)
	}
	if initResp.Error != nil {
		t.Fatalf("initialize returned error: %+v", initResp.Error)
	}
	result, ok := initResp.Result.(map[string]any)
	if !ok {
		t.Fatalf("initialize result shape: %T", initResp.Result)
	}
	if v, _ := result["protocolVersion"].(float64); int(v) != acpProtocolVersion {
		t.Fatalf("protocolVersion = %v, want %d", result["protocolVersion"], acpProtocolVersion)
	}

	// Unimplemented methods must answer with JSON-RPC method-not-found
	// rather than silently hanging the editor.
	var promptResp acpResponse
	if err := dec.Decode(&promptResp); err != nil {
		t.Fatalf("decode prompt response: %v", err)
	}
	if promptResp.Error == nil || promptResp.Error.Code != -32601 {
		t.Fatalf("session/prompt error = %+v, want code -32601", promptResp.Error)
	}

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("runACPMode did not exit after client hung up")
	}
}

// TestACPNotificationGetsNoReply pins the JSON-RPC rule that id-less requests
// are notifications and must not be answered.
func TestACPNotificationGetsNoReply(t *testing.T) {
	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}

	origOut := os.Stdout
	os.Stdout = outW
	t.Cleanup(func() { os.Stdout = origOut })
	withStdin(t, inR)

	go func() {
		_, _ = inW.WriteString(`{"jsonrpc":"2.0","method":"session/cancel"}` + "\n")
		inW.Close()
	}()

	go func() {
		runACPMode(config.Defaults(), nil)
		outW.Close()
	}()

	got, err := io.ReadAll(outR)
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("notification produced a reply: %q", got)
	}
}
