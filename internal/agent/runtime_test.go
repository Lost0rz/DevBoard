package agent

import (
	"bytes"
	"github.com/Lost0rz/DevBoard/internal/state"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func runtimeFixture(t *testing.T) (RuntimePaths, *state.Store, *Reducer) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "runtime")
	p := RuntimePaths{Dir: dir, Socket: filepath.Join(dir, "activity.sock")}
	st := state.NewStore(state.LiveInitialState(time.Now().UTC(), state.HostState{ID: "h"}))
	r := NewReducer(st, ReducerConfig{})
	return p, st, r
}
func TestRuntimeDirectoryAndSocketModes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	p, _, r := runtimeFixture(t)
	s, err := StartIngestServer(p, r)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	di, _ := os.Stat(p.Dir)
	si, _ := os.Stat(p.Socket)
	if di.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode=%o", di.Mode().Perm())
	}
	if si.Mode().Perm() != 0o600 {
		t.Fatalf("sock mode=%o", si.Mode().Perm())
	}
}
func TestRefuseRegularFileAndSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	for _, kind := range []string{"file", "dir", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			p, _, r := runtimeFixture(t)
			if err := os.MkdirAll(p.Dir, 0o700); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "file":
				_ = os.WriteFile(p.Socket, []byte("keep"), 0o600)
			case "dir":
				_ = os.Mkdir(p.Socket, 0o700)
			case "symlink":
				target := filepath.Join(p.Dir, "target")
				_ = os.WriteFile(target, []byte("keep"), 0o600)
				_ = os.Symlink(target, p.Socket)
			}
			if _, err := StartIngestServer(p, r); err == nil {
				t.Fatal("unexpected takeover")
			}
			if _, err := os.Lstat(p.Socket); err != nil {
				t.Fatal("path was deleted")
			}
		})
	}
}
func TestStaleSocketRecoverableAndLiveSocketProtected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	p, _, r := runtimeFixture(t)
	_ = os.MkdirAll(p.Dir, 0o700)
	addr := &net.UnixAddr{Name: p.Socket, Net: "unix"}
	ln, err := net.ListenUnix("unix", addr)
	if err != nil {
		t.Fatal(err)
	}
	ln.SetUnlinkOnClose(false)
	_ = ln.Close()
	s, err := StartIngestServer(p, r)
	if err != nil {
		t.Fatalf("stale recovery: %v", err)
	}
	defer s.Close()
	if _, err := StartIngestServer(p, r); err == nil {
		t.Fatal("second server stole live socket")
	}
}
func TestOversizedReadRejected(t *testing.T) {
	_, err := ReadBounded(bytes.NewReader(bytes.Repeat([]byte{'x'}, MaxRawProviderBytes+1)), MaxRawProviderBytes)
	if err == nil {
		t.Fatal("oversized input accepted")
	}
}
func TestOversizedNormalizedEventRejected(t *testing.T) {
	p, _, r := runtimeFixture(t)
	s, err := StartIngestServer(p, r)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c, err := net.Dial("unix", p.Socket)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_, _ = c.Write(bytes.Repeat([]byte{'x'}, MaxIPCEventBytes+1))
	_ = c.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	buf := make([]byte, 32)
	if n, _ := c.Read(buf); n != 0 {
		t.Fatalf("unexpected ack %q", buf[:n])
	}
}
func TestSendEventDaemonUnavailable(t *testing.T) {
	p, _, _ := runtimeFixture(t)
	e := AgentEvent{SchemaVersion: 1, EventID: "e", Provider: ProviderCodex, SessionID: "s", TurnID: ptrString("t"), EventType: EventUserPromptSubmit, OccurredAt: time.Now().UTC()}
	if err := SendEvent(p, e); err == nil {
		t.Fatal("expected transport error")
	}
}

func TestSendEventRoundTrip(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	p, st, r := runtimeFixture(t)
	s, err := StartIngestServer(p, r)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	e := AgentEvent{SchemaVersion: 1, EventID: "roundtrip", Provider: ProviderCodex, SessionID: "s", TurnID: ptrString("t"), EventType: EventUserPromptSubmit, OccurredAt: time.Now().UTC()}
	if err := SendEvent(p, e); err != nil {
		t.Fatal(err)
	}
	if len(st.Snapshot().Agents) != 1 || st.Snapshot().Agents[0].CurrentTurn.Activity != state.ActivityWorking {
		t.Fatalf("state=%+v", st.Snapshot().Agents)
	}
}
