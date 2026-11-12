// Devicecheck runs the installed backend against disposable synthetic data over
// a real AppLoad-compatible Unix sequenced-packet connection. It never connects
// to Gmail or reads the production account directory.
package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func main() {
	backend := flag.String("backend", "/home/root/xovi/exthome/appload/RemarkMail/backend/entry", "Installed backend")
	flag.Parse()
	if err := check(*backend); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func check(backend string) error {
	dir, err := os.MkdirTemp("", "remarkmail-check-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "socket")
	listener, err := net.ListenUnix("unixpacket", &net.UnixAddr{Name: path, Net: "unixpacket"})
	if err != nil {
		return err
	}
	defer listener.Close()
	_ = listener.SetDeadline(time.Now().Add(10 * time.Second))
	cmd := exec.Command(backend, "--demo", "--data-dir", filepath.Join(dir, "data"), path)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err = cmd.Start(); err != nil {
		return err
	}
	defer func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	conn, err := listener.AcceptUnix()
	if err != nil {
		return fmt.Errorf("Backend did not connect to the AppLoad test socket")
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	serial := 0
	rpc := func(action string, params any) (map[string]any, error) {
		serial++
		body, _ := json.Marshal(map[string]any{"id": serial, "action": action, "params": params})
		header := make([]byte, 8)
		binary.LittleEndian.PutUint32(header, 100)
		binary.LittleEndian.PutUint32(header[4:], uint32(len(body)))
		if n, e := conn.Write(header); e != nil || n != 8 {
			return nil, fmt.Errorf("Header write failed")
		}
		if n, e := conn.Write(body); e != nil || n != len(body) {
			return nil, fmt.Errorf("Body write failed")
		}
		if n, e := conn.Read(header); e != nil || n != 8 {
			return nil, fmt.Errorf("Header read failed")
		}
		if binary.LittleEndian.Uint32(header) != 101 {
			return nil, fmt.Errorf("Unexpected response type")
		}
		size := int(binary.LittleEndian.Uint32(header[4:]))
		if size < 1 || size > 180*1024 {
			return nil, fmt.Errorf("Unsafe response size")
		}
		body = make([]byte, size)
		if n, e := conn.Read(body); e != nil || n != size {
			return nil, fmt.Errorf("Body read failed")
		}
		var response map[string]any
		if json.Unmarshal(body, &response) != nil || int(response["id"].(float64)) != serial {
			return nil, fmt.Errorf("Response correlation failed")
		}
		return response, nil
	}
	ok := func(action string, params any) (any, error) {
		r, e := rpc(action, params)
		if e != nil {
			return nil, e
		}
		if r["ok"] != true {
			return nil, fmt.Errorf("%s failed: %v", action, r["error"])
		}
		return r["result"], nil
	}
	status, err := ok("status", map[string]any{})
	if err != nil {
		return err
	}
	if status.(map[string]any)["demo"] != true {
		return fmt.Errorf("Backend is not in isolated demo mode")
	}
	listing, err := ok("list", map[string]any{"folder": "inbox"})
	if err != nil {
		return err
	}
	if len(listing.(map[string]any)["threads"].([]any)) != 3 {
		return fmt.Errorf("Mailbox transport mismatch")
	}
	thread, err := ok("thread", map[string]any{"id": "sample-saturday"})
	if err != nil {
		return err
	}
	if len(thread.(map[string]any)["messages"].([]any)) != 2 {
		return fmt.Errorf("Conversation transport mismatch")
	}
	value, err := ok("draft-new", map[string]any{"draft": map[string]any{"to": "test@example.com", "subject": "Device check", "body": "Synthetic text only"}})
	if err != nil {
		return err
	}
	draft := value.(map[string]any)
	draft["body"] = "Unicode survives: café — ☕"
	draft["revision"] = 2
	if _, err = ok("draft-save", map[string]any{"draft": draft}); err != nil {
		return err
	}
	value, err = ok("draft-load", map[string]any{"id": draft["id"]})
	if err != nil || value.(map[string]any)["body"] != draft["body"] {
		return fmt.Errorf("Durable Unicode draft verification failed")
	}
	blocked, err := rpc("send", map[string]any{"id": draft["id"], "revision": 2, "confirm": false})
	if err != nil || blocked["ok"] != false {
		return fmt.Errorf("Unconfirmed send was not blocked")
	}
	if _, err = ok("draft-delete", map[string]any{"id": draft["id"]}); err != nil {
		return err
	}
	header := make([]byte, 8)
	binary.LittleEndian.PutUint32(header, 0xffffffff)
	if _, err = conn.Write(header); err != nil {
		return err
	}
	if err = cmd.Wait(); err != nil {
		return fmt.Errorf("Backend did not exit cleanly")
	}
	report := map[string]any{"arm_backend": true, "appload_seqpacket_transport": true, "mailbox_and_thread": true, "unicode_draft_persistence": true, "send_requires_confirmation": true, "clean_shutdown": true, "gmail_contacted": false, "production_account_accessed": false}
	b, _ := json.MarshalIndent(report, "", "  ")
	fmt.Println(string(b))
	return nil
}
