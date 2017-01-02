package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/ngaut/log"
)

var srvMode = flag.Bool("l", false, "server mode")

var (
	mapping = flag.String("m", "3333:4444", "local port:remote port")
	srvAddr = flag.String("s", "localhost:60010", "fwd server")
	desc    = flag.String("D", "default", "user defined description")
)

// sever
func doServer(srvAddr string) error {
	log.Info("start listen at", srvAddr)
	l, err := net.Listen("tcp", srvAddr)
	if err != nil {
		return err
	}
	defer l.Close()
	for {
		conn, err := l.Accept()
		if err != nil {
			return err
		}
		go handleFwdClient(conn)
	}
	return nil
}

func handleFwdClient(conn net.Conn) {
	for {
		p, err := readPacket(conn)
		if err != nil {
			log.Error(err)
			goto Cleanup
		}
		if p.hdr == hdrNewConn {
			// format: src-port:dst-port
			ports := strings.Split(string(p.args[0]), ":")
			srcPort, _ := strconv.Atoi(ports[0])
			dstPort, _ := strconv.Atoi(ports[1])
			desc := string(p.args[1])
			s := newService(conn, dstPort, srcPort, desc)
			services.Put(conn, s)
			go s.serve()
		}

		if p.hdr == hdrNewSession {
			// on new session
			id := binary.LittleEndian.Uint64(p.args[0])
			if connChan, ok := waitingSession.Get(id); ok {
				connChan.(chan net.Conn) <- conn
			}
			// no need to read packet anymore
			return
		}
	}
Cleanup:
	conn.Close()
	if s, ok := services.Get(conn); ok {
		s.(*service).stop()
	}
	services.Remove(conn)
	return
}

// client
func doClient(srvAddr string, srcPort, dstPort int, desc string) error {
	conn, err := net.Dial("tcp", srvAddr)
	if err != nil {
		return err
	}
	defer conn.Close()
	// send newConn packet
	p := packet{
		hdr: hdrNewConn,
		args: [][]byte{
			[]byte(fmt.Sprintf("%d:%d", srcPort, dstPort)),
			[]byte(desc),
		},
	}
	err = writePacket(conn, p)
	if err != nil {
		log.Info("connection lost")
		return err
	}

	for {
		p, err := readPacket(conn)
		if err != nil {
			log.Info("connection lost")
			return err
		}

		go func(c net.Conn, p *packet) {
			if p.hdr == hdrNewSession {
				sessionId := binary.LittleEndian.Uint64(p.args[0])
				log.Info("on new session", sessionId)
				// create tunnel
				tc, err := net.Dial("tcp", srvAddr)
				if err != nil {
					return err
				}

				b := make([]byte, 8)
				binary.LittleEndian.PutUint64(b, sessionId)
				if err := writePacket(tc, packet{
					hdr:  hdrNewSession,
					args: [][]byte{b},
				}); err != nil {
					log.Fatal(err)
				}
				t := tunnel{srcPort}
				t.handle(tc)
			}
		}(conn, p)
	}

	return nil
}

func main() {
	flag.Parse()

	if *srvMode {
		doServer(*srvAddr)
	} else {
		// connect to server
		// once server accept an outside connection, client need to create a
		// new local connection to srcAddr,
		ports := strings.Split(*mapping, ":")
		srcPort, _ := strconv.Atoi(ports[0])
		dstPort, _ := strconv.Atoi(ports[1])
		if err := doClient(*srvAddr, srcPort, dstPort, *desc); err != nil {
			log.Fatal(err)
		}
	}
}
