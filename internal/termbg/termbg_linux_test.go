//go:build linux

package termbg_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// openPTY returns the master side and the slave side of a new pseudo
// terminal, so a child process sees a real terminal on its standard streams.
func openPTY(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pty available: %v", err)
	}
	n, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		t.Fatal(err)
	}
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { master.Close(); slave.Close() })
	return master, slave
}

// runOnTerminal runs this test binary as a child on a pty and returns what it
// wrote. answer, when set, plays the terminal's part for background queries.
func runOnTerminal(t *testing.T, mode string, answer func(master *os.File, seen []byte)) (string, time.Duration) {
	t.Helper()
	master, slave := openPTY(t)
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), "ZN_TERMBG_CHILD="+mode, "TERM=xterm-256color", "COLORFGBG=")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.SysProcAttr = &unix.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	start := time.Now()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	slave.Close()
	var out bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, err := master.Read(buf)
			if n > 0 {
				out.Write(buf[:n])
				if answer != nil {
					answer(master, out.Bytes())
				}
			}
			if err != nil {
				return
			}
		}
	}()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(8 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("child did not exit; output so far: %q", out.String())
	}
	elapsed := time.Since(start)
	master.Close()
	wg.Wait()
	return out.String(), elapsed
}

// A terminal that never answers OSC 11 must not hold every command for
// termenv's five-second timeout, and no query may be written at all.
func TestStartupDoesNotQueryTheTerminal(t *testing.T) {
	out, elapsed := runOnTerminal(t, "startup", nil)
	if !strings.Contains(out, "child-ok") {
		t.Fatalf("child output: %q", out)
	}
	if strings.Contains(out, "\x1b]11;?") || strings.Contains(out, "\x1b[6n") {
		t.Fatalf("startup still queries the terminal: %q", out)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("startup took %s on a terminal that does not answer background queries", elapsed)
	}
}

// Detect is the deliberate query: it asks and believes the terminal.
func TestDetectAsksTheTerminalOnce(t *testing.T) {
	var replied sync.Once
	out, _ := runOnTerminal(t, "detect", func(master *os.File, seen []byte) {
		if bytes.Contains(seen, []byte("\x1b[6n")) {
			replied.Do(func() {
				_, _ = master.Write([]byte("\x1b]11;rgb:ffff/ffff/ffff\x1b\\\x1b[1;1R"))
			})
		}
	})
	if !strings.Contains(out, "light") {
		t.Fatalf("Detect ignored the terminal's light background: %q", out)
	}
	if strings.Count(out, "\x1b]11;?") != 1 {
		t.Fatalf("expected exactly one background query: %q", out)
	}
}
