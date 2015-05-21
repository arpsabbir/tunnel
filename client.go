package streamtunnel

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"net/http"

	"github.com/hashicorp/yamux"
)

// Client is responsible for creating a control connection to a tunnel server,
// creating new tunnels and proxy them to tunnel server.
type Client struct {
	// underlying tcp connection which is used for multiplexing
	nc net.Conn

	// underlying yamux session
	session *yamux.Session

	// serverAddr is the address of the tunnel-server
	serverAddr string

	// localAddr is the address of a local server that will be tunneled to the
	// public. Currently only one server is supported.
	localAddr string
}

// NewClient creates a new tunnel that is established between the serverAddr
// and localAddr. It exits if it can't create a new control connection to the
// server.
func NewClient(serverAddr, localAddr string) *Client {
	client := &Client{
		serverAddr: serverAddr,
		localAddr:  localAddr,
	}

	return client
}

func (c *Client) Start(identifier string) error {
	var err error
	c.nc, err = net.Dial("tcp", c.serverAddr)
	if err != nil {
		return err
	}

	remoteAddr := fmt.Sprintf("http://%s%s", c.nc.RemoteAddr(), TunnelPath)
	req, err := http.NewRequest("CONNECT", remoteAddr, nil)
	if err != nil {
		return fmt.Errorf("CONNECT %s", err)
	}

	req.Header.Set(XKTunnelIdentifier, identifier)
	if err := req.Write(c.nc); err != nil {
		return err
	}

	resp, err := http.ReadResponse(bufio.NewReader(c.nc), req)
	if err != nil {
		return fmt.Errorf("read response %s", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 && resp.Status != Connected {
		return fmt.Errorf("proxy server: %s", resp.Status)
	}

	c.session, err = yamux.Client(c.nc, yamux.DefaultConfig())
	if err != nil {
		return err
	}

	stream, err := c.session.Open()
	if err != nil {
		return err
	}

	if _, err := stream.Write([]byte(ctHandshakeRequest)); err != nil {
		return err
	}

	buf := make([]byte, len(ctHandshakeResponse))
	if _, err := stream.Read(buf); err != nil {
		return err
	}

	if string(buf) != ctHandshakeResponse {
		return fmt.Errorf("handshake aborted. got: %s", string(buf))
	}

	ct := newControl(stream)
	go c.listenControl(ct)

	log.Println("start is successfull.")
	return nil
}

func (c *Client) listenControl(ct *control) {
	for {
		var msg map[string]interface{}
		err := ct.dec.Decode(&msg)
		if err != nil {
			log.Println("decode err: %s", err)
			return
		}

		fmt.Printf("msg = %+v\n", msg)
	}
}