package zns

import (
	"encoding/binary"
	"io"
	"log"
	"net"
	"time"

	"github.com/miekg/dns"
)

// UpstreamPlain makes a DNS query over TCP or UDP (with TCP fallback)
type UpstreamPlain struct {
	Server  string
	Timeout time.Duration
}

func (p *UpstreamPlain) Query(m *dns.Msg) ([]byte, error) {
	udp, err := net.Dial("udp", p.Server)
	if err != nil {
		return nil, err
	}

	udp.SetDeadline(time.Now().Add(p.Timeout))
	defer udp.Close()

	data, err := m.Pack()
	if err != nil {
		return nil, err
	}

	if _, err := udp.Write(data); err != nil {
		return nil, err
	}

	reply := dns.Msg{}
	buf := make([]byte, 1500)
	for {
		n, err := udp.Read(buf)
		if err != nil {
			return nil, err
		}

		data := buf[:n]

		if err = reply.Unpack(data); err != nil {
			return nil, err
		}

		if reply.Id != m.Id { // not the response to our query
			continue
		} else if !reply.Truncated {
			return data, nil
		} else { // truncated, dial TCP againg
			break
		}
	}

	log.Printf("Truncated reply from %s for %s over UDP, retrying over TCP", p.Server, m.Question[0].String())

	tcp, err := net.Dial("tcp", p.Server)
	if err != nil {
		return nil, err
	}

	tcp.SetDeadline(time.Now().Add(p.Timeout))
	defer tcp.Close()

	var length uint16
	if err := binary.Read(tcp, binary.BigEndian, &length); err != nil {
		return nil, err
	}

	buf = make([]byte, int(length))
	n, err := io.ReadFull(tcp, buf)
	if err != nil {
		return nil, err
	}

	return buf[:n], nil
}
