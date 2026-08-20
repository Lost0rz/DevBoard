package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"time"
)

const (
	MaxRawProviderBytes = 4 << 20
	MaxIPCEventBytes    = 64 << 10
)

type RuntimePaths struct {
	Dir    string
	Socket string
}

func ResolveRuntimePaths() (RuntimePaths, error) {
	if override := os.Getenv("DEVBOARD_RUNTIME_DIR"); override != "" {
		abs, err := filepath.Abs(override)
		if err != nil {
			return RuntimePaths{}, err
		}
		return RuntimePaths{Dir: abs, Socket: filepath.Join(abs, "activity.sock")}, nil
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return RuntimePaths{}, err
	}
	dir := filepath.Join(cache, "devboard")
	return RuntimePaths{Dir: dir, Socket: filepath.Join(dir, "activity.sock")}, nil
}

func ReadBounded(r io.Reader, limit int64) ([]byte, error) {
	lr := io.LimitReader(r, limit+1)
	b, err := io.ReadAll(lr)
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("input exceeds %d bytes", limit)
	}
	return b, nil
}

type IngestServer struct {
	ln      *net.UnixListener
	path    string
	reducer *Reducer
	closed  chan struct{}
}

func StartIngestServer(paths RuntimePaths, reducer *Reducer) (*IngestServer, error) {
	if reducer == nil {
		return nil, errors.New("reducer required")
	}
	if err := ensureRuntimeDir(paths.Dir); err != nil {
		return nil, err
	}
	if err := prepareSocketPath(paths.Socket); err != nil {
		return nil, err
	}
	addr := &net.UnixAddr{Name: paths.Socket, Net: "unix"}
	ln, err := net.ListenUnix("unix", addr)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(paths.Socket, 0o600); err != nil {
		ln.Close()
		os.Remove(paths.Socket)
		return nil, err
	}
	s := &IngestServer{ln: ln, path: paths.Socket, reducer: reducer, closed: make(chan struct{})}
	go s.serve()
	return s, nil
}
func ensureRuntimeDir(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		return os.Chmod(dir, 0o700)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("runtime path is not a directory")
	}
	return os.Chmod(dir, 0o700)
}

type socketProbe func(string) error

func prepareSocketPath(path string) error {
	return prepareSocketPathWithProbe(path, probeUnixSocket)
}

func probeUnixSocket(path string) error {
	c, err := net.DialTimeout("unix", path, 100*time.Millisecond)
	if err != nil {
		return err
	}
	_ = c.Close()
	return nil
}

func prepareSocketPathWithProbe(path string, probe socketProbe) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("refusing to replace non-socket runtime path")
	}

	probeErr := probe(path)
	if probeErr == nil {
		return fmt.Errorf("live DevBoard listener already owns socket")
	}
	if !errors.Is(probeErr, syscall.ECONNREFUSED) {
		return fmt.Errorf("refusing to replace socket after ambiguous probe failure: %w", probeErr)
	}

	current, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if current.Mode()&os.ModeSymlink != 0 || current.Mode()&os.ModeSocket == 0 || !sameFileIdentity(info, current) {
		return fmt.Errorf("refusing to replace socket path changed during stale probe")
	}
	return os.Remove(path)
}

func sameFileIdentity(before, after os.FileInfo) bool {
	return os.SameFile(before, after) && reflect.DeepEqual(before.Sys(), after.Sys())
}
func (s *IngestServer) serve() {
	defer close(s.closed)
	for {
		c, err := s.ln.AcceptUnix()
		if err != nil {
			return
		}
		go s.handle(c)
	}
}
func (s *IngestServer) handle(c *net.UnixConn) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(500 * time.Millisecond))
	b, err := ReadBounded(c, MaxIPCEventBytes)
	if err != nil {
		return
	}
	var e AgentEvent
	if err := json.Unmarshal(bytes.TrimSpace(b), &e); err != nil {
		return
	}
	if err := e.Validate(); err != nil {
		return
	}
	if err := s.reducer.Submit(e); err != nil {
		return
	}
	_, _ = io.WriteString(c, "{\"ok\":true}\n")
}
func (s *IngestServer) Close() error {
	err := s.ln.Close()
	<-s.closed
	info, statErr := os.Lstat(s.path)
	if statErr == nil && info.Mode()&os.ModeSocket != 0 {
		_ = os.Remove(s.path)
	}
	return err
}

func SendEvent(paths RuntimePaths, e AgentEvent) error {
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if len(b) > MaxIPCEventBytes {
		return fmt.Errorf("normalized event too large")
	}
	payload := append(b, '\n')
	var last error
	for attempt := 0; attempt < 2; attempt++ {
		if err := sendOnce(paths.Socket, payload); err == nil {
			return nil
		} else {
			last = err
		}
	}
	return last
}
func sendOnce(path string, payload []byte) error {
	c, err := net.DialTimeout("unix", path, 100*time.Millisecond)
	if err != nil {
		return err
	}
	defer c.Close()
	_ = c.SetWriteDeadline(time.Now().Add(150 * time.Millisecond))
	if _, err := c.Write(payload); err != nil {
		return err
	}
	if uc, ok := c.(*net.UnixConn); ok {
		if err := uc.CloseWrite(); err != nil {
			return err
		}
	}
	_ = c.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
	ack, err := ReadBounded(c, 1024)
	if err != nil {
		return err
	}
	var v struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(ack), &v); err != nil || !v.OK {
		return fmt.Errorf("invalid ack")
	}
	return nil
}
