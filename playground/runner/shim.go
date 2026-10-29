package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

// shim runs inside a jail beside the program: it starts prog with PORT set,
// and once the program listens on 127.0.0.1:port it serves the unix socket
// sock, each connection bridged to that port. It exits as the program does.
func shim(sock, port, prog string) int {
	c := exec.Command(prog)
	c.Env = append(os.Environ(), "PORT="+port)
	c.Stdin, c.Stdout, c.Stderr = nil, os.Stdout, os.Stderr
	if err := c.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "runner:", err)
		return 1
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, os.Interrupt)
	go func() {
		for s := range sig {
			c.Process.Signal(s)
		}
	}()
	done := make(chan struct{})
	go func() {
		c.Wait()
		close(done)
	}()
	target := net.JoinHostPort("127.0.0.1", port)
	go func() {
		for {
			if conn, err := net.DialTimeout("tcp", target, time.Second); err == nil {
				conn.Close()
				break
			}
			select {
			case <-done:
				return
			case <-time.After(20 * time.Millisecond):
			}
		}
		os.Remove(sock)
		ln, err := net.Listen("unix", sock)
		if err != nil {
			fmt.Fprintln(os.Stderr, "runner:", err)
			c.Process.Kill()
			return
		}
		for {
			in, err := ln.Accept()
			if err != nil {
				return
			}
			go bridge(in, target)
		}
	}()
	<-done
	if c.ProcessState != nil {
		return c.ProcessState.ExitCode()
	}
	return 1
}

func bridge(in net.Conn, target string) {
	defer in.Close()
	out, err := net.Dial("tcp", target)
	if err != nil {
		return
	}
	defer out.Close()
	go func() {
		io.Copy(out, in)
		out.(*net.TCPConn).CloseWrite()
	}()
	io.Copy(in, out)
}
