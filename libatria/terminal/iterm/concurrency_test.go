package iterm

import (
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gorilla/websocket"
	pb "github.com/sethdeckard/atria/libatria/terminal/iterm/proto"
	"google.golang.org/protobuf/proto"
)

// startFakeITerm serves the iTerm2 API handshake on a Unix socket and answers
// every request with an empty response carrying the request's id. It returns
// the socket path and a counter of accepted WebSocket connections.
func startFakeITerm(t *testing.T) (string, *atomic.Int32) {
	t.Helper()
	// Unix socket paths are limited to about 100 bytes; t.TempDir is too long.
	dir, err := os.MkdirTemp("", "iterm")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	var conns atomic.Int32
	up := websocket.Upgrader{Subprotocols: []string{"api.iterm2.com"}}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		conns.Add(1)
		defer ws.Close()
		for {
			_, data, err := ws.ReadMessage()
			if err != nil {
				return
			}
			var req pb.ClientOriginatedMessage
			if err := proto.Unmarshal(data, &req); err != nil {
				return
			}
			resp := &pb.ServerOriginatedMessage{
				Id:         proto.Int64(req.GetId()),
				Submessage: &pb.ServerOriginatedMessage_ListSessionsResponse{ListSessionsResponse: &pb.ListSessionsResponse{}},
			}
			out, _ := proto.Marshal(resp)
			if err := ws.WriteMessage(websocket.BinaryMessage, out); err != nil {
				return
			}
		}
	})}
	go srv.Serve(ln) //nolint:errcheck // returns when the listener closes
	t.Cleanup(func() { srv.Close() })
	return sock, &conns
}

// Concurrent first calls must share one connection without data races.
// Run with -race to check synchronization of connection state.
func TestConcurrentCallsShareOneConnection(t *testing.T) {
	sock, conns := startFakeITerm(t)
	c := NewClient(Options{SocketPath: sock, NoPrompt: true})
	t.Cleanup(func() { _ = c.Close() }) // srv.Close doesn't reach hijacked WebSocket connections

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := c.Available(); err != nil {
				t.Errorf("Available: %v", err)
			}
		}()
	}
	wg.Wait()
	if got := conns.Load(); got != 1 {
		t.Errorf("fake iTerm2 saw %d connections, want 1 shared connection", got)
	}
}

// Close racing with in-flight calls must not race on the connection either;
// the call after Close reconnects on demand.
func TestCloseDuringCallsIsSafe(t *testing.T) {
	sock, _ := startFakeITerm(t)
	c := NewClient(Options{SocketPath: sock, NoPrompt: true})
	t.Cleanup(func() { _ = c.Close() })
	if err := c.Available(); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = c.Available() // may fail while a Close is in flight; only the race matters
		}()
		go func() {
			defer wg.Done()
			_ = c.Close()
		}()
	}
	wg.Wait()
	if err := c.Available(); err != nil {
		t.Errorf("Available after Close should reconnect: %v", err)
	}
}
