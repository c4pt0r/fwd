package main

import (
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/juju/errors"
	"github.com/ngaut/log"
)

var sessionId uint64
var waitingSession = newConcurrentMap()
var services = newConcurrentMap()

type service struct {
	port        int
	srcPort     int
	desc        string
	controlConn net.Conn
	mu          sync.Mutex
	conns       map[net.Conn]struct{}
	ch          chan struct{}
}

func newService(c net.Conn, port int, srcPort int, desc string) *service {
	return &service{
		controlConn: c,
		port:        port,
		desc:        desc,
		conns:       make(map[net.Conn]struct{}),
		ch:          make(chan struct{}),
	}
}

func (s *service) stop() {
	close(s.ch)
}

func (s *service) serve() error {
	laddr, err := net.ResolveTCPAddr("tcp", fmt.Sprintf(":%d", s.port))
	if err != nil {
		return errors.Trace(err)
	}
	listener, err := net.ListenTCP("tcp", laddr)
	if err != nil {
		return errors.Trace(err)
	}
	log.Info("create new service successfully", s.srcPort, ":", s.port, s.desc)
	for {
		select {
		case <-s.ch:
			log.Info("stopping listening on", listener.Addr())
			// clean ongoing connections
			listener.Close()
			s.mu.Lock()
			for c, _ := range s.conns {
				c.Close()
			}
			s.conns = nil
			s.mu.Unlock()

			return nil
		default:
		}
		listener.SetDeadline(time.Now().Add(1 * time.Second))
		conn, err := listener.AcceptTCP()
		if err != nil {
			if opErr, ok := err.(*net.OpError); ok && opErr.Timeout() {
				continue
			}
			log.Error(err)
		} else {
			go s.handle(conn)
		}
	}
}

func createTunnel(c net.Conn, sessionId uint64) (net.Conn, error) {
	// create waiting stub
	connChan := make(chan net.Conn)
	waitingSession.Put(sessionId, connChan)

	// cleanup
	defer func() {
		close(connChan)
		waitingSession.Remove(sessionId)
	}()

	log.Info("send new tunnel request")

	// tell client to connect
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, sessionId)
	if err := writePacket(c, packet{
		hdr:  hdrNewSession,
		args: [][]byte{b},
	}); err != nil {
		return nil, errors.Trace(err)
	}

	// wait for response
	var cc net.Conn
	select {
	case cc = <-connChan:
		{
			return cc, nil
		}
	case <-time.After(10 * time.Second):
		{
			return nil, errors.New("create tunnel timeout")
		}
	}
}

func (s *service) cleanupConn(c net.Conn) {
	s.mu.Lock()
	c.Close()
	delete(s.conns, c)
	s.mu.Unlock()
}

func (s *service) handle(c net.Conn) {
	defer s.cleanupConn(c)

	s.mu.Lock()
	s.conns[c] = struct{}{}
	s.mu.Unlock()

	cc, err := createTunnel(s.controlConn, atomic.AddUint64(&sessionId, 1))
	if err != nil {
		log.Error(err)
		return
	}
	proxy(c, cc)
}
