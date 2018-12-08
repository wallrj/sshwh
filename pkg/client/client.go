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

func (c *Client) Open() (string, error) {
	config := &ssh.ClientConfig{
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		BannerCallback:  c.bannerCallback,
	}
	client, err := ssh.Dial("tcp", c.config.Address, config)
	if err != nil {
		return "", errors.Wrap(err, "failed to dial")
	}
	c.sshClient = client
	return c.sessionID, nil
}

func (c *Client) Send(sessionID string, content io.Reader) error {
	session, err := c.sshClient.NewSession()
	if err != nil {
		return errors.Wrap(err, "failed to create session")
	}
	defer func() {
		err := session.Close()
		if err != nil {
			log.Println(errors.Wrap(err, "error in session.Close"))
		}
	}()

	session.Stdin = content
	if err := session.Run(sessionID); err != nil {
		return errors.Wrap(err, "failed to send session ID")
	}
	return nil
}

func (c *Client) Receive(content io.Writer) error {
	session, err := c.sshClient.NewSession()
	if err != nil {
		return errors.Wrap(err, "failed to create session")
	}
	defer func() {
		err := session.Close()
		if err != nil {
			log.Println(errors.Wrap(err, "error in session.Close"))
		}
	}()
	session.Stdout = content
	err = session.Shell()
	if err != nil {
		return errors.Wrap(err, "failed to start shell")
	}
	err = session.Wait()
	if err != nil {
		return errors.Wrap(err, "failed during wait")
	}
	return nil
}

func (c *Client) Close() error {
	return c.sshClient.Close()
}
