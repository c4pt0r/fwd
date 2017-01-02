package main

import (
	"fmt"
	"io"
	"net"
	"sync"

	"github.com/ngaut/log"
)

var tunnelBuffer = sync.Pool{
	New: func() interface{} {
		return make([]byte, 1<<16)
	},
}

type tunnel struct {
	// TODO: use addr?
	localPort int
}

func (t *tunnel) handle(c net.Conn) {
	// TODO: use TLS dialer?
	localConn, err := net.Dial("tcp", fmt.Sprintf("localhost:%d", t.localPort))
	if err != nil {
		// occurs error, close remote conn
		c.Close()
		return
	}
	log.Info("accepted", c.RemoteAddr(), "to", localConn.LocalAddr())
	proxy(c, localConn)
}

func proxy(c net.Conn, c1 net.Conn) {
	// only close connections for once.
	first := make(chan<- struct{}, 1)

	cp := func(dst net.Conn, src net.Conn) {
		buf := tunnelBuffer.Get().([]byte)
		defer tunnelBuffer.Put(buf)
		// copy data
		_, err := io.CopyBuffer(dst, src, buf)
		select {
		case first <- struct{}{}:
			if err != nil {
				log.Error(err)
			}
			dst.Close()
			src.Close()
			log.Info("disconnect from", c.RemoteAddr())
		default:
		}
	}
	go cp(c, c1)
	cp(c1, c)
}
