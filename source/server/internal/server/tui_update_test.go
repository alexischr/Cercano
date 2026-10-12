package server

import (
	"context"
	"net"
	"testing"
	"time"

	"cercano/source/server/pkg/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

func TestTUIUpdateFanoutAndReconnectSnapshot(t *testing.T) {
	s := &Server{events: newEventHub()}
	listener := bufconn.Listen(1 << 20)
	g := grpc.NewServer()
	proto.RegisterAgentServer(g, s)
	go g.Serve(listener)
	t.Cleanup(func() { g.Stop(); listener.Close() })
	conn, e := grpc.NewClient("passthrough:///fixture", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := proto.NewAgentClient(conn)
	first, e := client.SubscribeEvents(ctx, &proto.SubscribeEventsRequest{})
	if e != nil {
		t.Fatal(e)
	}
	second, e := client.SubscribeEvents(ctx, &proto.SubscribeEventsRequest{})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.NotifyTUIUpdateInstalled("1.2.3", 9); e != nil {
		t.Fatal(e)
	}
	for _, stream := range []proto.Agent_SubscribeEventsClient{first, second} {
		ev, e := stream.Recv()
		if e != nil {
			t.Fatal(e)
		}
		u := ev.GetTuiUpdateInstalled()
		if u.GetVersion() != "1.2.3" || u.GetOperationId() != 9 {
			t.Fatal("wrong notification", ev)
		}
	}
	if e = s.NotifyTUIUpdateInstalled("1.2.3", 9); e != nil {
		t.Fatal("duplicate rejected", e)
	}
	if e = s.NotifyTUIUpdateInstalled("1.2.4", 9); e == nil {
		t.Fatal("conflicting operation accepted")
	}
	if e = s.NotifyTUIUpdateInstalled("1.2.2", 8); e == nil {
		t.Fatal("stale operation accepted")
	}
	if e = s.NotifyTUIUpdateInstalled("1.2.4", 10); e != nil {
		t.Fatal(e)
	}
	for _, stream := range []proto.Agent_SubscribeEventsClient{first, second} {
		ev, e := stream.Recv()
		if e != nil {
			t.Fatal(e)
		}
		if ev.GetTuiUpdateInstalled().GetOperationId() != 10 {
			t.Fatal("duplicate/stale notification", ev)
		}
	}
	late, e := client.SubscribeEvents(ctx, &proto.SubscribeEventsRequest{})
	if e != nil {
		t.Fatal(e)
	}
	ev, e := late.Recv()
	if e != nil || ev.GetTuiUpdateInstalled().GetOperationId() != 10 {
		t.Fatal("late subscriber missed installed state", ev, e)
	}
	// Installing a notice never launches/stops processes and doesn't turn the
	// existing event stream into a command/path channel.
}
func TestTUIUpdateRefusesUnusableMetadata(t *testing.T) {
	s := &Server{events: newEventHub()}
	for _, v := range []string{"", "../client", "/tmp/client", "version\nrestart", "hello world"} {
		if e := s.NotifyTUIUpdateInstalled(v, 1); e == nil {
			t.Fatal("invalid metadata", v)
		}
	}
	if e := s.NotifyTUIUpdateInstalled("1.0", 0); e == nil {
		t.Fatal("zero operation accepted")
	}
	if s.events.latestTUI != nil {
		t.Fatal("invalid notice persisted")
	}
}
