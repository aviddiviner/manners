package manners

import (
	"crypto/tls"
	"fmt"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

var listenerClosed = fmt.Errorf("listener is closed")

// NewListener wraps an existing listener for use with
// GracefulServer.
//
// Note that you generally don't need to use this directly as
// GracefulServer will automatically wrap any non-graceful listeners
// supplied to it.
func NewListener(l net.Listener) *GracefulListener {
	return &GracefulListener{listener: l}
}

// A GracefulListener differs from a standard net.Listener in one way: if
// Accept() is called after it is gracefully closed, it returns a
// listenerAlreadyClosed error. The GracefulServer will ignore this error.
type GracefulListener struct {
	listener  net.Listener
	closeOnce sync.Once
	closed    int32 // accessed atomically
}

func (l *GracefulListener) isClosed() bool {
	return atomic.LoadInt32(&l.closed) == 1
}

func (l *GracefulListener) Addr() net.Addr {
	return l.listener.Addr()
}

// Accept implements the Accept method in the Listener interface.
func (l *GracefulListener) Accept() (net.Conn, error) {
	if l.isClosed() {
		return nil, listenerClosed
	}
	return l.listener.Accept()
}

// Close tells the wrapped listener to stop listening.  It is idempotent.
func (l *GracefulListener) Close() (err error) {
	l.closeOnce.Do(func() {
		atomic.StoreInt32(&l.closed, 1)
		err = l.listener.Close()
	})
	return
}

func (l *GracefulListener) GetFile() (*os.File, error) {
	return getListenerFile(l.listener)
}

func (l *GracefulListener) Clone() (net.Listener, error) {
	if l.isClosed() {
		return nil, listenerClosed
	}

	file, err := l.GetFile()
	if err != nil {
		return nil, err
	}
	defer file.Close()

	fl, err := net.FileListener(file)
	if nil != err {
		return nil, err
	}
	return fl, nil
}

// A listener implements a network listener (net.Listener) for TLS connections.
// direct lift from crypto/tls.go
type TLSListener struct {
	net.Listener
	config *tls.Config
}

// Accept waits for and returns the next incoming TLS connection.
// The returned connection c is a *tls.Conn.
func (l *TLSListener) Accept() (c net.Conn, err error) {
	c, err = l.Listener.Accept()
	if err != nil {
		return
	}
	c = tls.Server(c, l.config)
	return
}

// NewListener creates a Listener which accepts connections from an inner
// Listener and wraps each connection with Server.
// The configuration config must be non-nil and must have
// at least one certificate.
func NewTLSListener(inner net.Listener, config *tls.Config) net.Listener {
	l := new(TLSListener)
	l.Listener = inner
	l.config = config
	return l
}

// TCPKeepAliveListener sets TCP keep-alive timeouts on accepted
// connections. It's used by ListenAndServe and ListenAndServeTLS so
// dead TCP connections (e.g. closing laptop mid-download) eventually
// go away.
//
// direct lift from net/http/server.go
type TCPKeepAliveListener struct {
	*net.TCPListener
}

func (ln TCPKeepAliveListener) Accept() (c net.Conn, err error) {
	tc, err := ln.AcceptTCP()
	if err != nil {
		return
	}
	tc.SetKeepAlive(true)
	tc.SetKeepAlivePeriod(3 * time.Minute)
	return tc, nil
}

func getListenerFile(listener net.Listener) (*os.File, error) {
	switch t := listener.(type) {
	case *net.TCPListener:
		return t.File()
	case *net.UnixListener:
		return t.File()
	case TCPKeepAliveListener:
		return t.TCPListener.File()
	case *TLSListener:
		return getListenerFile(t.Listener)
	}
	return nil, fmt.Errorf("Unsupported listener: %T", listener)
}
