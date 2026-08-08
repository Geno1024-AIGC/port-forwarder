package forward

import (
	"io"
	"net"
	"sync"
)

// Relay copies data in both directions between the two connections
// until both directions are done, then closes both connections.
func Relay(a, b net.Conn) {
	var wg sync.WaitGroup
	var once sync.Once

	terminate := func() {
		once.Do(func() {
			_ = a.Close()
			_ = b.Close()
		})
	}

	pipe := func(dst, src net.Conn) {
		defer wg.Done()
		_, _ = io.Copy(dst, src)
		closeWriteHalf(dst)
		terminate()
	}

	wg.Add(2)
	go pipe(a, b)
	go pipe(b, a)
	wg.Wait()
}

// closeWriteHalf closes the write half of a TCP connection so the peer
// sees EOF, while still allowing the read half to complete.
func closeWriteHalf(c net.Conn) {
	if tcp, ok := c.(*net.TCPConn); ok {
		_ = tcp.CloseWrite()
	} else {
		_ = c.Close()
	}
}