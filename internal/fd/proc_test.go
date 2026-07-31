package fd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// writeLimits writes a minimal /proc/[pid]/limits file.
func writeLimits(t *testing.T, root string, pid, soft, hard int) {
	t.Helper()
	content := fmt.Sprintf(
		"Limit                     Soft Limit           Hard Limit           Units\n"+
			"Max cpu time              unlimited            unlimited            seconds\n"+
			"Max open files            %-20d %-20d files\n",
		soft, hard,
	)
	path := filepath.Join(root, strconv.Itoa(pid), "limits")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// writeNetTCP writes a minimal /proc/[pid]/net/tcp file.
// Each entry is [localPort, state, inode].
func writeNetTCP(t *testing.T, root string, pid int, entries [][3]uint64) {
	t.Helper()
	netDir := filepath.Join(root, strconv.Itoa(pid), "net")
	if err := os.MkdirAll(netDir, 0755); err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	sb.WriteString("  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n")
	for i, e := range entries {
		port, state, inode := e[0], e[1], e[2]
		// Build "XXXXXXXX:PPPP" format (little-endian IP doesn't matter for port parsing)
		sb.WriteString(fmt.Sprintf(
			"  %2d: 00000000:%04X 00000000:0000 %02X 00000000:00000000 00:00000000 00000000     0        0 %d 1 0000000000000000 100 0 0 10 0\n",
			i, port, state, inode,
		))
	}
	if err := os.WriteFile(filepath.Join(netDir, "tcp"), []byte(sb.String()), 0644); err != nil {
		t.Fatal(err)
	}
}

// addFDSymlink creates a symlink in /proc/[pid]/fd/.
// target can be e.g. "socket:[123]" or "/etc/nginx/nginx.conf" or "pipe:[456]".
func addFDSymlink(t *testing.T, root string, pid int, fdNum int, target string) {
	t.Helper()
	fdDir := filepath.Join(root, strconv.Itoa(pid), "fd")
	if err := os.MkdirAll(fdDir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(fdDir, strconv.Itoa(fdNum))
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
}

func TestReadFDLimit(t *testing.T) {
	root := t.TempDir()
	writeLimits(t, root, 100, 1024, 4096)

	n, err := readFDLimit(root, 100)
	if err != nil {
		t.Fatalf("readFDLimit: %v", err)
	}
	if n != 1024 {
		t.Errorf("got %d, want 1024", n)
	}
}

func TestReadFDLimit_Unlimited(t *testing.T) {
	root := t.TempDir()
	pid := 200
	content := "Limit                     Soft Limit           Hard Limit           Units\n" +
		"Max open files            unlimited            unlimited            files\n"
	path := filepath.Join(root, strconv.Itoa(pid), "limits")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	n, err := readFDLimit(root, pid)
	if err != nil {
		t.Fatalf("readFDLimit unlimited: %v", err)
	}
	if n != 1<<31-1 {
		t.Errorf("got %d, want MaxInt32", n)
	}
}

func TestParseHexPort(t *testing.T) {
	cases := []struct {
		in   string
		want uint16
	}{
		{"00000000:0050", 80},
		{"0F020000:1F90", 8080},
		{"0F020000:01BB", 443},
	}
	for _, tc := range cases {
		got, err := parseHexPort(tc.in)
		if err != nil {
			t.Errorf("parseHexPort(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parseHexPort(%q): got %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestParseSocketInode(t *testing.T) {
	cases := []struct {
		in     string
		want   uint64
		wantOK bool
	}{
		{"socket:[123456]", 123456, true},
		{"socket:[0]", 0, true},
		{"/etc/nginx/nginx.conf", 0, false},
		{"pipe:[789]", 0, false},
		{"anon_inode:[eventfd]", 0, false},
	}
	for _, tc := range cases {
		got, ok := parseSocketInode(tc.in)
		if ok != tc.wantOK {
			t.Errorf("parseSocketInode(%q): ok=%v, want %v", tc.in, ok, tc.wantOK)
		}
		if ok && got != tc.want {
			t.Errorf("parseSocketInode(%q): got %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestReadCounts_Classification(t *testing.T) {
	pid := 300
	root := t.TempDir()

	writeLimits(t, root, pid, 1024, 4096)

	// tcp entries: [localPort, state, inode]
	writeNetTCP(t, root, pid, [][3]uint64{
		{80, tcpListen, 100001},     // LISTEN on :80 → Other
		{80, 0x01, 100002},          // ESTABLISHED, local :80 → ClientSocket
		{80, 0x01, 100003},          // ESTABLISHED, local :80 → ClientSocket (2nd)
		{45620, 0x01, 100004},       // ESTABLISHED, local :45620 → UpstreamSocket
		{45621, 0x01, 100005},       // ESTABLISHED, local :45621 → UpstreamSocket
	})

	// fd entries
	addFDSymlink(t, root, pid, 0, "socket:[100001]") // listen → Other
	addFDSymlink(t, root, pid, 3, "socket:[100002]") // client
	addFDSymlink(t, root, pid, 4, "socket:[100003]") // client
	addFDSymlink(t, root, pid, 5, "socket:[100004]") // upstream
	addFDSymlink(t, root, pid, 6, "socket:[100005]") // upstream
	addFDSymlink(t, root, pid, 7, "/etc/nginx/nginx.conf") // file
	addFDSymlink(t, root, pid, 8, "pipe:[999]")            // other

	counts, err := ReadCounts(root, pid)
	if err != nil {
		t.Fatalf("ReadCounts: %v", err)
	}

	if counts.FDLimit != 1024 {
		t.Errorf("FDLimit: got %d, want 1024", counts.FDLimit)
	}
	if counts.FDCount != 7 {
		t.Errorf("FDCount: got %d, want 7", counts.FDCount)
	}
	if counts.ClientSockets != 2 {
		t.Errorf("ClientSockets: got %d, want 2", counts.ClientSockets)
	}
	if counts.UpstreamSockets != 2 {
		t.Errorf("UpstreamSockets: got %d, want 2", counts.UpstreamSockets)
	}
	if counts.Files != 1 {
		t.Errorf("Files: got %d, want 1", counts.Files)
	}
	// listen socket (100001) + pipe (999 not in tcp table) = 2 Other
	if counts.Other != 2 {
		t.Errorf("Other: got %d, want 2", counts.Other)
	}
}

func TestReadCounts_Pct(t *testing.T) {
	pid := 400
	root := t.TempDir()
	writeLimits(t, root, pid, 1000, 2000)
	writeNetTCP(t, root, pid, nil)

	// 100 file FDs → 10%
	fdDir := filepath.Join(root, strconv.Itoa(pid), "fd")
	if err := os.MkdirAll(fdDir, 0755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		target := fmt.Sprintf("/fake/file/%d", i)
		if err := os.Symlink(target, filepath.Join(fdDir, strconv.Itoa(i))); err != nil {
			t.Fatal(err)
		}
	}

	counts, err := ReadCounts(root, pid)
	if err != nil {
		t.Fatalf("ReadCounts: %v", err)
	}
	if counts.FDCount != 100 {
		t.Errorf("FDCount: got %d, want 100", counts.FDCount)
	}
	got := counts.Pct()
	if got != 10.0 {
		t.Errorf("Pct: got %.2f, want 10.00", got)
	}
}
