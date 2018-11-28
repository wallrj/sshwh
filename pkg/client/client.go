package client

import (
	"bytes"
	"log"

	"github.com/pkg/errors"
	"golang.org/x/crypto/ssh"
)

type Client struct {
	Address   string
	sshClient *ssh.Client
}

func (c *Client) Open() error {
	config := &ssh.ClientConfig{
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		BannerCallback: func(message string) error {
			log.Println("BANNER", message)
			return nil
		},
	}
	client, err := ssh.Dial("tcp", c.Address, config)
	if err != nil {
		return errors.Wrap(err, "failed to dial")
	}
	defer client.Close()
	// Each ClientConn can support multiple interactive sessions,
	// represented by a Session.
	session, err := client.NewSession()
	if err != nil {
		return errors.Wrap(err, "failed to create session")
	}
	defer session.Close()

	// Once a Session is created, you can execute a single command on
	// the remote side using the Run method.
	var b bytes.Buffer
	session.Stdout = &b
	if err := session.Run("/usr/bin/whoami"); err != nil {
		return errors.Wrap(err, "failed to run")
	}
	log.Println("output", b.String())
	return nil
}
