package zns

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
)

// parseMasqueTarget parses the target UDP address
//
// https://example.org/.well-known/masque/udp/{target_host}/{target_port}/
// https://proxy.example.org:4443/masque?h={target_host}&p={target_port}
// https://proxy.example.org:4443/masque{?target_host,target_port}
//
// See https://www.rfc-editor.org/rfc/rfc9298.html#name-client-configuration
func parseMasqueTarget(target *url.URL) (addr string, err error) {
	var host, port string
	switch {
	case strings.HasPrefix(target.Path, "/.well-known/masque/udp/"):
		s := target.Path[len("/.well-known/masque/udp/"):]
		i := strings.Index(s, "/")
		if i == -1 {
			err = errors.New("invalid target")
			return
		}
		host = s[:i]
		port = strings.TrimRight(s[i+1:], "/")
	case strings.HasPrefix(target.Path, "/masque"):
		host = target.Query().Get("h")
		port = target.Query().Get("p")
		if host == "" {
			var s string
			s, err = url.QueryUnescape(target.RawQuery)
			if err != nil {
				err = errors.New("invalid target")
				return
			}

			i := strings.Index(s, ",")
			if i == -1 {
				err = errors.New("invalid target")
				return
			}
			host = s[:i]
			port = s[i+1:]
		}
	default:
		err = errors.New("invalid target")
		return
	}

	if _, err = strconv.Atoi(port); err != nil {
		err = errors.New("invalid port")
		return
	}

	if host == "" {
		err = errors.New("invalid host")
		return
	}

	if strings.Contains(host, ":") {
		addr = "[" + host + "]:" + port
	} else {
		addr = host + ":" + port
	}

	return
}

func parseContextID(d []byte) (int, int) {
	if len(d) == 0 {
		return 0, 0
	}
	id := int(d[0])
	p := id >> 6
	l := 1 << p

	id = id & 0x3f
	for i := 1; i < l; i++ {
		id = id<<8 + int(d[i])
	}
	return id, l
}
