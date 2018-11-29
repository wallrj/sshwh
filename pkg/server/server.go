package server

import (
	"encoding/base64"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"sync"
	"time"

	"github.com/pkg/errors"
	"golang.org/x/crypto/ssh"
)

type Config struct {
	Address     string
	HostKeyPath string
}

type Consumer struct {
	sessionID   string
	writeCloser io.WriteCloser
	finished    chan struct{}
}

type Server struct {
	config          *Config
	listener        *net.TCPListener
	sshServerConfig *ssh.ServerConfig
	connections     sync.WaitGroup
	quit            chan struct{}
	exited          chan struct{}
	consumers       map[string]*Consumer
}

func (s *Server) bannerCallback(conn ssh.ConnMetadata) string {
	sessionID := base64.StdEncoding.EncodeToString(conn.SessionID())
	log.Println("SESSIONID", sessionID)
	return fmt.Sprintf("cat FILE > ssh localhost %s\n", sessionID)
}

func New(c *Config) (*Server, error) {
	server := &Server{
		config:    c,
		quit:      make(chan struct{}),
		exited:    make(chan struct{}),
		consumers: map[string]*Consumer{},
	}
	sshConfig := &ssh.ServerConfig{
		NoClientAuth:   true,
		BannerCallback: server.bannerCallback,
	}
	privateBytes, err := ioutil.ReadFile(c.HostKeyPath)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read host key file")
	}

	private, err := ssh.ParsePrivateKey(privateBytes)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse private key")
	}
	sshConfig.AddHostKey(private)
	server.sshServerConfig = sshConfig
	return server, nil
}

func (s *Server) Open() error {
	log.Println("Server.Open")
	addr, err := net.ResolveTCPAddr("tcp4", s.config.Address)
	if err != nil {
		return errors.Wrap(err, "failed to resolve TCP address")
	}
	listener, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return errors.Wrap(err, "failed to listen for connection")
	}
	s.listener = listener
	return nil
}

func (s *Server) Serve() error {
	log.Println("Server.Serve")
	defer close(s.exited)
	for {
		select {
		case <-s.quit:
			log.Println("shutting down gracefully")
			err := s.listener.Close()
			log.Println("waiting for connections to end")
			s.connections.Wait()
			return err
		default:
			err := s.listener.SetDeadline(time.Now().Add(1e9))
			if err != nil {
				return errors.Wrap(err, "failed to set deadline")
			}
			conn, err := s.listener.Accept()
			if err != nil {
				if opErr, ok := err.(*net.OpError); ok && opErr.Timeout() {
					continue
				}
				return errors.Wrap(err, "Failed to accept connection")
			}
			s.connections.Add(1)
			go func() {
				defer s.connections.Done()
				err := s.handleConnection(conn)
				if err != nil {
					log.Println(errors.Wrap(err, "failed to handle connection"))
				}
			}()
		}
	}
}

type exitStatusMsg struct {
	Status uint32
}

// RFC 4254 Section 6.5.
type execMsg struct {
	Command string
}

func (s *Server) handleConnection(conn net.Conn) error {
	defer conn.Close()
	serverConn, chans, reqs, err := ssh.NewServerConn(conn, s.sshServerConfig)
	if err != nil {
		return errors.Wrap(err, "failed to handshake")
	}
	defer serverConn.Close()

	sessionID := base64.StdEncoding.EncodeToString(serverConn.SessionID())

	go ssh.DiscardRequests(reqs)
	for c := range chans {
		log.Println("NewChannel.ChannelType", c.ChannelType())
		log.Printf("NewChannel.ExtraData: %q\n", c.ExtraData())
		chn, reqs, err := c.Accept()
		if err != nil {
			log.Println(errors.Wrap(err, "failed to accept"))
			continue
		}

		go func() {
			defer chn.Close()
			for req := range reqs {
				log.Println("req", req.Type)
				switch req.Type {
				case "exec":
					var msg execMsg
					err := ssh.Unmarshal(req.Payload, &msg)
					if err != nil {
						log.Println(errors.Wrap(err, "failed to unmarshal"))
					}
					consumer, found := s.consumers[msg.Command]
					if !found {
						req.Reply(false, nil)
						log.Printf("unexpected command: %q\n", msg.Command)
						return
					}
					defer close(consumer.finished)
					_, err = io.Copy(consumer.writeCloser, chn)
					if err != nil {
						log.Println(errors.Wrap(err, "failed to copy"))
					}
					return
				case "shell":
					consumer := &Consumer{
						sessionID:   sessionID,
						finished:    make(chan struct{}),
						writeCloser: chn,
					}
					s.consumers[sessionID] = consumer
					<-consumer.finished
					return
				default:
				}
				err := req.Reply(true, nil)
				if err != nil {
					log.Println(errors.Wrap(err, "failed to reply"))
				}
			}
		}()
	}
	return nil
}

func (s *Server) Close() error {
	log.Println("Server.Close")
	defer log.Println("Server.Closed")
	close(s.quit)
	<-s.exited
	return nil
}
