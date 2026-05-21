package peek_test

import "net"

func netDialImpl(network, addr string) (closableConn, error) {
	c, err := net.Dial(network, addr)
	if err != nil {
		return nil, err
	}
	return c, nil
}
