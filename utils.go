package main

import (
	"encoding/binary"
	"io"
	"net"
	"sync"

	"github.com/juju/errors"
)

/*

Controll packet data frame

+--------+-------------------+----------------+-----------+----------------+-----------+-----+
| type   | sizeof(arguments) | len(argument1) | argument1 | len(argument2) | argument2 | ... |
+--------+-------------------+----------------+-----------+----------------+-----------+-----+
| 1 byte | 1 byte            | 4 bytes        | ...       | 4 bytes        | ...       | ... |
+--------+-------------------+----------------+-----------+----------------+-----------+-----+

<---------- header ---------><------------------------- args -------------------------------->

*/

type headerType byte

const (
	hdrNewConn headerType = 0xc4 + iota
	hdrNewSession
)

type packet struct {
	hdr  headerType
	args [][]byte
}

func (p *packet) bytes() []byte {
	buf := make([]byte, 0)
	buf = append(buf, byte(p.hdr))
	buf = append(buf, byte(len(p.args)))

	for _, arg := range p.args {
		b := make([]byte, 4)
		binary.LittleEndian.PutUint32(b, uint32(len(arg)))

		buf = append(buf, b...)
		buf = append(buf, arg...)
	}
	return buf
}

// | size: uint64 | buf: bytes |
func readPacket(conn net.Conn) (*packet, error) {
	header := make([]byte, 2)
	if _, err := io.ReadFull(conn, header); err != nil {
		return nil, errors.Trace(err)
	}
	p := &packet{
		hdr: headerType(header[0]),
	}
	sz := int(header[1])
	if sz > 0 {
		args := make([][]byte, 0)
		for i := 0; i < sz; i++ {
			szBuf := make([]byte, 4)
			if _, err := io.ReadFull(conn, szBuf); err != nil {
				return nil, errors.Trace(err)
			}
			argSize := binary.LittleEndian.Uint32(szBuf)
			argBuf := make([]byte, argSize)
			if _, err := io.ReadFull(conn, argBuf); err != nil {
				return nil, errors.Trace(err)
			}
			args = append(args, argBuf)
		}
		p.args = args
	}
	return p, nil
}

func writePacket(conn net.Conn, pkt packet) error {
	b := pkt.bytes()
	if _, err := conn.Write(b); err != nil {
		return errors.Trace(err)
	}
	return nil
}

// concurrent map

type concurrentMap struct {
	mu   sync.RWMutex
	data map[interface{}]interface{}
}

func newConcurrentMap() *concurrentMap {
	return &concurrentMap{
		data: make(map[interface{}]interface{}),
	}
}

func (m *concurrentMap) Put(k, v interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[k] = v
}

func (m *concurrentMap) Get(k interface{}) (interface{}, bool) {
	m.mu.RLock()
	if v, ok := m.data[k]; ok {
		m.mu.RUnlock()
		return v, true
	}
	m.mu.RUnlock()
	return nil, false
}

func (m *concurrentMap) Remove(k interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, k)
}
