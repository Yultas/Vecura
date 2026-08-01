package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func main() {
	pidFlag := flag.Int("pid", 0, "PID of the running application")
	source := flag.String("source", "", "downloaded update executable")
	target := flag.String("target", "", "currently installed executable")
	flag.Parse()
	if *pidFlag <= 0 || *source == "" || *target == "" {
		fatal("pid, source and target are required")
	}

	if err := waitForParent(*pidFlag, 30*time.Second); err != nil {
		fatal(err.Error())
	}
	backup := *target + ".bak"
	if err := replaceWithRollback(*source, *target, backup); err != nil {
		fatal(err.Error())
	}
	newPID, err := start(*target)
	if err != nil {
		_ = rollback(*target, backup)
		fatal(err.Error())
	}
	// A process can start successfully and then exit immediately because of a
	// missing DLL, invalid config, or a broken release. Keep the backup until
	// the new process has survived a short startup window.
	time.Sleep(3 * time.Second)
	if !processListed(newPID) {
		if rollbackErr := rollback(*target, backup); rollbackErr != nil {
			fatal(fmt.Sprintf("updated application exited and rollback failed: %v", rollbackErr))
		}
		if _, startErr := start(*target); startErr != nil {
			fatal(fmt.Sprintf("updated application exited; old version restored but could not restart: %v", startErr))
		}
		fatal("updated application exited during startup; previous version restored")
	}
	_ = os.Remove(backup)
	_ = os.Remove(*source)
}

func waitForParent(pid int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		// Renaming the target is the authoritative Windows lock check; this
		// process only needs to avoid racing the parent while it exits.
		if !processListed(pid) {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("application process %d did not exit", pid)
}

func processListed(pid int) bool {
	out, err := exec.Command("tasklist", "/FI", "PID eq "+strconv.Itoa(pid), "/NH").Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), strconv.Itoa(pid))
}

func replaceWithRollback(source, target, backup string) error {
	_ = os.Remove(backup)
	var moved bool
	var lastErr error
	for i := 0; i < 150; i++ {
		if err := os.Rename(target, backup); err == nil {
			moved = true
			break
		} else {
			lastErr = err
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !moved {
		return fmt.Errorf("cannot move old application: %w", lastErr)
	}
	if err := os.Rename(source, target); err != nil {
		_ = os.Rename(backup, target)
		return fmt.Errorf("cannot install update: %w", err)
	}
	return nil
}

func start(target string) (int, error) {
	cmd := exec.Command(target)
	cmd.Dir = filepath.Dir(target)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("start updated application: %w", err)
	}
	return cmd.Process.Pid, nil
}

func rollback(target, backup string) error {
	if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(backup, target); err != nil {
		return err
	}
	return nil
}

func fatal(message string) {
	if message == "" {
		message = "update failed"
	}
	fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}
