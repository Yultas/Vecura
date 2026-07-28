package llama

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// Server manages a background llama-server process for VL embeddings.
type Server struct {
	rm       *RuntimeManager
	cmd      *exec.Cmd
	pid      int
	port     int
	baseDir  string
	mu       sync.Mutex // protects cmd, pid
	stopping int32      // atomic; set by Stop to abort WaitForReady
}

// NewServer creates a server manager using the given runtime.
func NewServer(rm *RuntimeManager) *Server {
	return &Server{
		rm:      rm,
		port:    8090, // default port for VL embeddings
		baseDir: rm.BaseDir(),
	}
}

// Port returns the port the server listens on.
func (s *Server) Port() int { return s.port }

// SetPort overrides the default port (must be called before Start).
func (s *Server) SetPort(p int) { s.port = p }

// SetLogFunc overrides the logging function (called from api.Startup to emit via Wails).
func (s *Server) SetLogFunc(fn func(string, ...interface{})) {
	if s.rm != nil {
		s.rm.Logf = fn
	}
}

// Start launches llama-server in the background. If a server is already
// running (stale PID file or live process), it returns nil.
// mmprojPath may be empty — when omitted the model file must contain the
// vision projector (e.g. some bundled GGUFs).
func (s *Server) Start(modelPath, mmprojPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.isRunningLocked() {
		return nil
	}
	atomic.StoreInt32(&s.stopping, 0)
	serverPath := s.rm.ServerPath()
	if serverPath == "" {
		return fmt.Errorf("llama-server not installed")
	}
	if _, err := os.Stat(modelPath); err != nil {
		return fmt.Errorf("model not found: %w", err)
	}
	if mmprojPath != "" {
		if _, err := os.Stat(mmprojPath); err != nil {
			return fmt.Errorf("mmproj not found: %w", err)
		}
	}

	binDir := s.rm.BinDir()
	logPath := filepath.Join(s.baseDir, "server.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		return fmt.Errorf("create log: %w", err)
	}

	args := []string{
		"--model", modelPath,
		"--port", strconv.Itoa(s.port),
		"--host", "127.0.0.1",
		"--embeddings",
	}
	if mmprojPath != "" {
		args = append(args, "--mmproj", mmprojPath)
	}

	s.cmd = exec.Command(serverPath, args...)
	s.cmd.Dir = binDir
	s.cmd.Stdout = logFile
	s.cmd.Stderr = logFile
	s.cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | 0x08000000, // CREATE_NO_WINDOW
	}

	s.rm.Logf("[llama] starting server port=%d model=%s", s.port, modelPath)
	if err := s.cmd.Start(); err != nil {
		s.rm.Logf("[llama] server start failed: %v", err)
		logFile.Close()
		return fmt.Errorf("start server: %w", err)
	}
	s.pid = s.cmd.Process.Pid
	s.savePID()
	s.rm.Logf("[llama] server started PID=%d port=%d", s.pid, s.port)

	// Wait in background to reap the process.
	cmd := s.cmd
	pid := s.pid
	go func() {
		_ = cmd.Wait()
		s.rm.Logf("[llama] server PID=%d exited", pid)
		s.cleanupPID()
	}()

	return nil
}

// Stop sends a graceful termination signal and waits up to 5 seconds before
// force-killing. Safe to call when the server is not running.
func (s *Server) Stop() error {
	atomic.StoreInt32(&s.stopping, 1)

	s.mu.Lock()
	defer s.mu.Unlock()

	pid := s.loadPID()
	if pid == 0 && s.cmd == nil {
		return nil
	}
	if pid == 0 && s.cmd != nil {
		pid = s.cmd.Process.Pid
	}

	s.rm.Logf("[llama] stopping server PID=%d...", pid)
	proc, err := os.FindProcess(pid)
	if err != nil {
		s.cleanupPID()
		return nil
	}

	// Try graceful shutdown via WM_CLOSE / os.Interrupt.
	_ = proc.Signal(os.Interrupt)

	cmd := s.cmd
	done := make(chan struct{})
	go func() {
		if cmd != nil {
			_ = cmd.Wait()
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = proc.Kill()
	}

	s.cleanupPID()
	s.cmd = nil
	return nil
}

// IsRunning reports whether the llama-server process is alive.
func (s *Server) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.isRunningLocked()
}

// isRunningLocked is the internal version that assumes the mutex is held.
func (s *Server) isRunningLocked() bool {
	pid := s.loadPID()
	if pid == 0 {
		return false
	}
	return isProcessAlive(pid)
}

// WaitForReady polls the server's health endpoint until it responds or
// timeout elapses. Returns early if Stop() is called.
func (s *Server) WaitForReady(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}
	url := fmt.Sprintf("http://127.0.0.1:%d/health", s.port)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&s.stopping) != 0 {
			return fmt.Errorf("server start cancelled")
		}
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return nil
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("server did not become ready within %s", timeout)
}

func (s *Server) pidPath() string {
	return filepath.Join(s.baseDir, "server.pid")
}

func (s *Server) savePID() {
	os.WriteFile(s.pidPath(), []byte(strconv.Itoa(s.pid)), 0o644)
}

func (s *Server) loadPID() int {
	data, err := os.ReadFile(s.pidPath())
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	return pid
}

func (s *Server) cleanupPID() {
	os.Remove(s.pidPath())
}

func isProcessAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}
