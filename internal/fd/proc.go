package fd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Counts holds the FD breakdown for one worker process.
type Counts struct {
	WorkerPID       int
	FDCount         int
	FDLimit         int
	ClientSockets   int
	UpstreamSockets int
	Files           int
	Other           int
}

// Pct returns FDCount as a percentage of FDLimit.
func (c Counts) Pct() float64 {
	if c.FDLimit == 0 {
		return 0
	}
	return float64(c.FDCount) / float64(c.FDLimit) * 100
}

// ReadCounts reads FD usage for the given pid from procRoot.
func ReadCounts(procRoot string, pid int) (Counts, error) {
	c := Counts{WorkerPID: pid}

	limit, err := readFDLimit(procRoot, pid)
	if err != nil {
		return c, err
	}
	c.FDLimit = limit

	listenPorts, inodeInfo, err := readSocketTable(procRoot, pid)
	if err != nil {
		return c, err
	}

	fdDir := filepath.Join(procRoot, strconv.Itoa(pid), "fd")
	entries, err := os.ReadDir(fdDir)
	if err != nil {
		return c, fmt.Errorf("read %s: %w", fdDir, err)
	}

	for _, e := range entries {
		target, err := os.Readlink(filepath.Join(fdDir, e.Name()))
		if err != nil {
			continue // fd may have closed
		}

		inode, ok := parseSocketInode(target)
		if !ok {
			if strings.HasPrefix(target, "/") {
				c.Files++
			} else {
				c.Other++
			}
			c.FDCount++
			continue
		}

		c.FDCount++
		si, known := inodeInfo[inode]
		if !known {
			c.Other++
			continue
		}
		if si.state == tcpListen {
			c.Other++ // listen socket; not a connection
			continue
		}
		if listenPorts[si.localPort] {
			c.ClientSockets++
		} else {
			c.UpstreamSockets++
		}
	}

	return c, nil
}

// readFDLimit reads the soft open-file limit from /proc/[pid]/limits.
func readFDLimit(procRoot string, pid int) (int, error) {
	f, err := os.Open(filepath.Join(procRoot, strconv.Itoa(pid), "limits"))
	if err != nil {
		return 0, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "Max open files") {
			continue
		}
		// "Max open files            1024                 4096                 files"
		fields := strings.Fields(line)
		if len(fields) < 4 {
			return 0, fmt.Errorf("pid %d: unexpected limits format: %q", pid, line)
		}
		// field[3] is the soft limit (field[4] is hard limit)
		v := fields[3]
		if v == "unlimited" {
			return 1<<31 - 1, nil
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			return 0, fmt.Errorf("pid %d: parse open-files limit %q: %w", pid, v, err)
		}
		return n, nil
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	return 0, fmt.Errorf("pid %d: Max open files not found in limits", pid)
}

const tcpListen = 0x0A

type socketInfo struct {
	localPort uint16
	state     uint8
}

// readSocketTable parses /proc/[pid]/net/tcp and tcp6, returning:
//   - listenPorts: set of local ports in LISTEN state (nginx's accept ports)
//   - inodeInfo:   map of socket inode → (localPort, state)
func readSocketTable(procRoot string, pid int) (listenPorts map[uint16]bool, inodeInfo map[uint64]socketInfo, err error) {
	listenPorts = map[uint16]bool{}
	inodeInfo = map[uint64]socketInfo{}

	for _, name := range []string{"tcp", "tcp6"} {
		path := filepath.Join(procRoot, strconv.Itoa(pid), "net", name)
		if err2 := parseNetTCP(path, listenPorts, inodeInfo); err2 != nil && !os.IsNotExist(err2) {
			err = fmt.Errorf("parse %s: %w", path, err2)
			return
		}
	}
	return
}

// parseNetTCP reads one /proc/[pid]/net/tcp or tcp6 file.
// Format (columns, 0-indexed): sl local_address rem_address st ... inode
//
//	1              3             5              ...  9
func parseNetTCP(path string, listenPorts map[uint16]bool, inodeInfo map[uint64]socketInfo) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Scan() // skip header
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 10 {
			continue
		}
		localPort, err := parseHexPort(fields[1])
		if err != nil {
			continue
		}
		state, err := strconv.ParseUint(fields[3], 16, 8)
		if err != nil {
			continue
		}
		inode, err := strconv.ParseUint(fields[9], 10, 64)
		if err != nil {
			continue
		}
		si := socketInfo{localPort: localPort, state: uint8(state)}
		inodeInfo[inode] = si
		if si.state == tcpListen {
			listenPorts[localPort] = true
		}
	}
	return scanner.Err()
}

// parseHexPort extracts the port from a "XXXXXXXX:PPPP" hex address field.
func parseHexPort(addrField string) (uint16, error) {
	parts := strings.SplitN(addrField, ":", 2)
	if len(parts) != 2 {
		return 0, fmt.Errorf("bad addr field %q", addrField)
	}
	port, err := strconv.ParseUint(parts[1], 16, 16)
	if err != nil {
		return 0, err
	}
	return uint16(port), nil
}

// parseSocketInode extracts the inode number from "socket:[N]" symlink targets.
func parseSocketInode(target string) (uint64, bool) {
	if !strings.HasPrefix(target, "socket:[") || !strings.HasSuffix(target, "]") {
		return 0, false
	}
	s := target[len("socket:[") : len(target)-1]
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}
