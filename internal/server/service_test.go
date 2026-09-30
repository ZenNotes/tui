package server

import (
	"context"
	"encoding/xml"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestServiceFilesEscapePathsAndKeepTokenOutOfArguments(t *testing.T) {
	dir := filepath.Join(t.TempDir(), `Notes 日本語 & %n $HOME`)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	s := ServiceSpec{Label: "com.zennotes.server.test", Executable: "/bin/true", Dir: dir, LogPath: filepath.Join(dir, "server.log"), Env: map[string]string{"ZENNOTES_AUTH_TOKEN_FILE": filepath.Join(dir, `notes "quotes" & %n $HOME`, "token")}}
	plist := launchdUnit(s)
	d := xml.NewDecoder(strings.NewReader(plist))
	for {
		_, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("invalid launchd XML: %v", err)
		}
	}
	unit := systemdUnit(s)
	if !strings.Contains(unit, "%%n") || !strings.Contains(unit, "ExecStart=:\"/bin/true\"") {
		t.Fatal("systemd substitutions not escaped")
	}
	if runtime.GOOS == "darwin" {
		path := filepath.Join(dir, "test.plist")
		if err := os.WriteFile(path, []byte(plist), 0o600); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command("plutil", "-lint", path).CombinedOutput(); err != nil {
			t.Fatalf("launchd plist: %s %v", out, err)
		}
	}
	if tool, err := exec.LookPath("systemd-analyze"); err == nil {
		path := filepath.Join(dir, s.Label+".service")
		if err := os.WriteFile(path, []byte(unit), 0o600); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if out, err := exec.CommandContext(ctx, tool, "verify", "--man=no", path).CombinedOutput(); err != nil {
			t.Fatalf("systemd unit: %s %v", out, err)
		}
	}
}
