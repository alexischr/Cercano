package modelhost

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	"cercano/source/server/internal/filelock"
	"cercano/source/server/pkg/proto"
)

const (
	defaultEnsureTimeout = 10 * time.Second
	probeTimeout         = 2 * time.Second
	pollInterval         = 100 * time.Millisecond
	replaceWait          = 5 * time.Second
)

// EnsureOptions configures Ensure.
type EnsureOptions struct {
	// RunDir holds the host record and launch lock. Default RunDir().
	RunDir string
	// ClientVersion is sent in Hello.
	ClientVersion string
	// Launch starts a host process; it must not wait for it to be ready.
	Launch func(ctx context.Context) error
	// Timeout bounds how long Ensure waits for a launched host. Default 10s.
	Timeout time.Duration
}

// Dial connects to the host named by r over loopback, attaching the host's
// token to every unary and streaming call.
func Dial(_ context.Context, r Record) (*grpc.ClientConn, error) {
	withToken := func(ctx context.Context) context.Context {
		return metadata.AppendToOutgoingContext(ctx, tokenHeader, r.Token)
	}
	return grpc.NewClient(r.Addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
			return invoker(withToken(ctx), method, req, reply, cc, opts...)
		}),
		grpc.WithStreamInterceptor(func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
			return streamer(withToken(ctx), desc, cc, method, opts...)
		}),
	)
}

// hello dials r and performs the Hello handshake.
func hello(ctx context.Context, r Record, clientVersion string) (*proto.HelloResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	conn, err := Dial(ctx, r)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	return proto.NewModelHostClient(conn).Hello(ctx, &proto.HelloRequest{ClientVersion: clientVersion})
}

func shutdownHost(ctx context.Context, r Record) error {
	ctx, cancel := context.WithTimeout(ctx, replaceWait)
	defer cancel()
	conn, err := Dial(ctx, r)
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = proto.NewModelHostClient(conn).Shutdown(ctx, &proto.ShutdownRequest{})
	return err
}

type probeState int

const (
	notLive probeState = iota
	live
	incompatible
)

// probe reads the record and classifies the host it names. Any read, dial or
// handshake failure (including a draining host or wrong token) is notLive.
func probe(ctx context.Context, dir, clientVersion string) (Record, probeState) {
	rec, err := ReadRecord(dir)
	if err != nil {
		return Record{}, notLive
	}
	resp, err := hello(ctx, rec, clientVersion)
	if err != nil {
		return rec, notLive
	}
	if int(resp.GetApiVersion()) == APIVersion {
		return rec, live
	}
	return rec, incompatible
}

// replaceIncompatible asks an incompatible host to shut down and waits for
// its record to disappear or for it to stop answering.
func replaceIncompatible(ctx context.Context, dir string, rec Record, clientVersion string) {
	_ = shutdownHost(ctx, rec)
	deadline := time.Now().Add(replaceWait)
	for time.Now().Before(deadline) && ctx.Err() == nil {
		cur, err := ReadRecord(dir)
		if err != nil || cur.PID != rec.PID || cur.Addr != rec.Addr {
			return
		}
		if _, err := hello(ctx, cur, clientVersion); err != nil {
			return
		}
		sleep(ctx, 50*time.Millisecond)
	}
}

func sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

// Ensure returns the record of a live, compatible model host, launching one
// if necessary. At most one Launch call is made per Ensure.
func Ensure(ctx context.Context, opts EnsureOptions) (Record, error) {
	dir := opts.RunDir
	if dir == "" {
		dir = RunDir()
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultEnsureTimeout
	}

	check := func() (Record, bool) {
		rec, st := probe(ctx, dir, opts.ClientVersion)
		switch st {
		case live:
			return rec, true
		case incompatible:
			replaceIncompatible(ctx, dir, rec, opts.ClientVersion)
		}
		return Record{}, false
	}

	if rec, ok := check(); ok {
		return rec, nil
	}
	if opts.Launch == nil {
		return Record{}, errors.New("model host is not running and no launcher is configured")
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Record{}, err
	}
	lockCtx, cancelLock := context.WithTimeout(ctx, timeout+replaceWait)
	release, err := filelock.Lock(lockCtx, filepath.Join(dir, "launch.lock"))
	cancelLock()
	if err != nil {
		return Record{}, fmt.Errorf("waiting for model host launch lock: %w", err)
	}
	defer release()

	if rec, ok := check(); ok {
		return rec, nil
	}
	if err := opts.Launch(ctx); err != nil {
		return Record{}, fmt.Errorf("launching model host: %w", err)
	}

	// The record may be absent, stale, or name a draining host until the
	// replacement publishes its own, so keep polling until the deadline.
	deadline := time.Now().Add(timeout)
	for {
		if rec, st := probe(ctx, dir, opts.ClientVersion); st == live {
			return rec, nil
		}
		if err := ctx.Err(); err != nil {
			return Record{}, err
		}
		if time.Now().After(deadline) {
			return Record{}, fmt.Errorf("model host did not become ready within %s", timeout)
		}
		sleep(ctx, pollInterval)
	}
}

// LaunchBinary returns a Launch func that spawns "<path> model-host"
// detached, appending its output to RunDir()/modelhost.log.
func LaunchBinary(path string) func(ctx context.Context) error {
	return func(context.Context) error {
		dir := RunDir()
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		logf, err := os.OpenFile(filepath.Join(dir, "modelhost.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return err
		}
		defer logf.Close()
		cmd := exec.Command(path, "model-host")
		cmd.Stdout = logf
		cmd.Stderr = logf
		cmd.SysProcAttr = detachSysProcAttr()
		if err := cmd.Start(); err != nil {
			return err
		}
		return cmd.Process.Release()
	}
}
