package client

import (
	"io"
	"log"

	"github.com/pkg/errors"
	"golang.org/x/crypto/ssh"
)

type Client struct {
	config    *Config
	sessionID string
	sshClient *ssh.Client
}

type Config struct {
	Address string
}

func New(c *Config) *Client {
	return &Client{config: c}
}

func (c *Client) bannerCallback(message string) error {
	log.Println("BANNER RECEIVED", message)
	c.sessionID = message
	return nil
}

func (c *Client) open() error {
	config := &ssh.ClientConfig{
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		BannerCallback:  c.bannerCallback,
	}
	client, err := ssh.Dial("tcp", c.config.Address, config)
	if err != nil {
		return errors.Wrap(err, "failed to dial")
	}
	c.sshClient = client
	return nil
}

func (c *Client) Send(content io.Reader) error {
	err := c.open()
	if err != nil {
		return errors.Wrap(err, "failed to open")
	}
	defer c.sshClient.Close()

	session, err := c.sshClient.NewSession()
	if err != nil {
		return errors.Wrap(err, "failed to create session")
	}
	defer session.Close()

	session.Stdin = content
	if err := session.Run(c.sessionID); err != nil {
		return errors.Wrap(err, "failed to send session ID")
	}
	return nil
}

func (c *Client) Receive(content io.Writer) error {
	return nil
}
