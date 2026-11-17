package main

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
)

// AppLoad uses a separate sequenced packet for the little-endian header and body.
func serve(conn *net.UnixConn, app *finder) error {
	defer conn.Close()
	var header [8]byte
	readPacket := func(dst []byte) error {
		// One extra byte detects a truncated or overlong packet on all platforms.
		buf := make([]byte, len(dst)+1)
		n, err := conn.Read(buf)
		if err != nil {
			return err
		}
		if n != len(dst) {
			return errors.New("unexpected AppLoad packet length")
		}
		copy(dst, buf[:n])
		return nil
	}
	for {
		if err := readPacket(header[:]); err != nil {
			return err
		}
		kind := binary.LittleEndian.Uint32(header[:4])
		size := binary.LittleEndian.Uint32(header[4:])
		if size > 256*1024 {
			return errors.New("request too large")
		}
		body := make([]byte, size)
		if size > 0 {
			if err := readPacket(body); err != nil {
				return err
			}
		}
		if kind == 0xffffffff {
			return nil
		}
		if kind != 100 {
			continue
		}
		out := app.reply(body)
		binary.LittleEndian.PutUint32(header[:], 101)
		binary.LittleEndian.PutUint32(header[4:], uint32(len(out)))
		for _, packet := range [][]byte{header[:], out} {
			n, err := conn.Write(packet)
			if err != nil {
				return err
			}
			if n != len(packet) {
				return io.ErrShortWrite
			}
		}
	}
}

func main() {
	root := flag.String("device-root", "/", "Device filesystem root for a local fixture")
	stdio := flag.Bool("stdio", false, "Use newline-delimited JSON for local verification")
	flag.Parse()
	fs, err := os.OpenRoot(*root)
	if err == nil {
		defer fs.Close()
		app := &finder{fs: fs}
		switch {
		case *stdio:
			s := bufio.NewScanner(os.Stdin)
			s.Buffer(make([]byte, 4096), 256*1024)
			for s.Scan() {
				if _, err = fmt.Println(string(app.reply(s.Bytes()))); err != nil {
					break
				}
			}
			if err == nil {
				err = s.Err()
			}
		case flag.NArg() == 1:
			var conn *net.UnixConn
			conn, err = net.DialUnix("unixpacket", nil, &net.UnixAddr{Name: flag.Arg(0), Net: "unixpacket"})
			if err == nil {
				err = serve(conn, app)
			}
		default:
			err = errors.New("supply an AppLoad socket or --stdio")
		}
	}
	if err != nil && !errors.Is(err, io.EOF) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type request struct {
	ID      int64  `json:"id"`
	Action  string `json:"action"`
	Place   string `json:"place"`
	Path    string `json:"path"`
	Query   string `json:"query"`
	Sort    string `json:"sort"`
	Hidden  bool   `json:"hidden"`
	Refresh bool   `json:"refresh"`
	Offset  int    `json:"offset"`
}

type reply struct {
	ID    int64  `json:"id"`
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

func (f *finder) reply(body []byte) []byte {
	var q request
	var res reply
	err := json.Unmarshal(body, &q)
	res.ID = q.ID
	if err == nil {
		res.Data, err = f.handle(q)
	}
	if err != nil {
		res.Error = err.Error()
		res.Data = nil
	}
	out, err := json.Marshal(res)
	if err != nil || len(out) > 176*1024 {
		out, _ = json.Marshal(reply{ID: q.ID, Error: "This result is too large to display."})
	}
	return out
}
