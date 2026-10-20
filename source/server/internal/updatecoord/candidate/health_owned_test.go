//go:build linux || windows || (darwin && cgo)

package candidate

import (
	"context"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"cercano/source/server/pkg/proto"
	"google.golang.org/grpc"
)

func TestHealthOwnedChild(t *testing.T) {
	if os.Getenv("CERCANO_HEALTH_FIXTURE") != "1" {
		return
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		os.Exit(3)
	}
	server := grpc.NewServer()
	proto.RegisterAgentServer(server, &healthFixture{response: &proto.GetAgentHealthResponse{Version: "fixture-health", Pid: int32(os.Getpid()), Ready: true}})
	go server.Serve(listener)
	if os.WriteFile(os.Getenv("CERCANO_HEALTH_ENDPOINT"), []byte(listener.Addr().String()), 0600) != nil {
		os.Exit(4)
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(os.Getenv("CERCANO_CANDIDATE_RELEASE")); err == nil {
			server.Stop()
			os.Exit(0)
		}
		time.Sleep(5 * time.Millisecond)
	}
	os.Exit(5)
}
func TestHealthOwnedCandidateObservation(t *testing.T) {
	if candidateTestHost(t) {
		return
	}
	opts, release := options(t)
	endpointFile := filepath.Join(t.TempDir(), "endpoint")
	opts.Argv = []string{"-test.run=^TestHealthOwnedChild$"}
	opts.Env = append(opts.Env, "CERCANO_HEALTH_FIXTURE=1", "CERCANO_HEALTH_ENDPOINT="+endpointFile)
	c, err := Start(opts)
	if c != nil {
		defer complete(t, c, release)
	}
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	var endpoint netip.AddrPort
	for ctx.Err() == nil {
		data, _ := os.ReadFile(endpointFile)
		endpoint, err = netip.ParseAddrPort(string(data))
		if err == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !endpoint.IsValid() {
		t.Fatal("fixture endpoint unavailable", ctx.Err())
	}
	// Native socket snapshots can race descriptor churn; each attempt remains
	// independent and no earlier positive evidence survives a later unknown sample.
	var observation Observation
	for ctx.Err() == nil {
		observation, err = c.ObserveHealth(ctx, endpoint, "fixture-health")
		if observation.Status == Consistent {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil || observation.Status != Consistent || observation.ReportedPID != int32(c.Process().Pid()) {
		t.Fatal(observation, err)
	}
	observation, err = c.ObserveHealth(ctx, endpoint, "wrong-release")
	if err != nil || observation.Status != Inconsistent {
		t.Fatal(observation, err)
	}
}
