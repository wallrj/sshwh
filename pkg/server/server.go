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
	sessionID string
	writer    io.Writer
	finished  chan struct{}
}

type Producer struct {
	sessionID string
	reader    io.Reader
	finished  chan struct{}
}

type Connection struct {
	sessionID string
	consumer  *Consumer
	producer  *Producer
	finished  chan struct{}
}

type Connector struct {
	connections map[string]*Connection
	consumers   chan *Consumer
	producers   chan *Producer
}

func NewConnector() *Connector {
	return &Connector{
		connections: map[string]*Connection{},
		consumers:   make(chan *Consumer),
		producers:   make(chan *Producer),
	}
}

func (c *Connector) handleConnection(connection *Connection) {
	defer close(connection.consumer.finished)
	defer close(connection.producer.finished)
	defer delete(c.connections, connection.sessionID)
	_, err := io.Copy(connection.consumer.writer, connection.producer.reader)
	if err != nil {
		log.Println(errors.Wrap(err, "failed to copy"))
	}
}

func (c *Connector) Start() {
	for {
		var (
			connection *Connection
			found      bool
			sessionID  string
		)
		select {
		case consumer := <-c.consumers:
			connection, found = c.connections[consumer.sessionID]
			connection.consumer = consumer
			sessionID = consumer.sessionID
		case producer := <-c.producers:
			connection, found = c.connections[producer.sessionID]
			connection.producer = producer
			sessionID = producer.sessionID
		}
		if found {
			c.handleConnection(connection)
		}
		c.connections[sessionID] = connection
	}
}

func (c *Connector) AddProducer(sessionID string, producer io.Reader) chan struct{} {
	o := &Producer{
		sessionID: sessionID,
		reader:    producer,
	}
	c.producers <- o
	return o.finished
}

func (c *Connector) AddConsumer(sessionID string, consumer io.Writer) chan struct{} {
	o := &Consumer{
		sessionID: sessionID,
		writer:    consumer,
	}
	c.consumers <- o
	return o.finished
}

type Server struct {
	config          *Config
	listener        *net.TCPListener
	sshServerConfig *ssh.ServerConfig
	connections     sync.WaitGroup
	quit            chan struct{}
	exited          chan struct{}
	connector       *Connector
}

func (s *Server) bannerCallback(conn ssh.ConnMetadata) string {
	sessionID := base64.StdEncoding.EncodeToString(conn.SessionID())
	log.Println("SESSIONID", sessionID)
	return fmt.Sprintf(sessionID)
}

func New(c *Config) (*Server, error) {
	server := &Server{
		config:    c,
		quit:      make(chan struct{}),
		exited:    make(chan struct{}),
		connector: NewConnector(),
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
	go s.connector.Start()
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
	serverConn, err := NewServerConn(conn, s.sshServerConfig, s.connector)
	if err != nil {
		return errors.Wrap(err, "failed to create NewServerConn")
	}
	defer func() {
		err := serverConn.Close()
		if err != nil {
			log.Println(errors.Wrap(err, "error in serverConn.Close"))
		}
	}()
	return nil
}

func (s *Server) Close() error {
	log.Println("Server.Close")
	defer log.Println("Server.Closed")
	close(s.quit)
	<-s.exited
	return nil
}

type OpenServerConn struct {
	sshServerConn *ssh.ServerConn
	newChannels   <-chan ssh.NewChannel
	requests      <-chan *ssh.Request
	connector     *Connector
}

func (s *OpenServerConn) Close() error {
	return s.sshServerConn.Close()
}

func NewServerConn(conn net.Conn, config *ssh.ServerConfig, connector *Connector) (*OpenServerConn, error) {
	sshServerConn, chans, reqs, err := ssh.NewServerConn(conn, config)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create ssh.NewServerConn")
	}

	serverConn := &OpenServerConn{
		sshServerConn: sshServerConn,
		newChannels:   chans,
		requests:      reqs,
		connector:     connector,
	}

	return serverConn, nil
}

func (s *OpenServerConn) Serve() error {
	go ssh.DiscardRequests(s.requests)

	var openChannels sync.WaitGroup
	for c := range s.newChannels {
		openChannels.Add(1)
		go func(c ssh.NewChannel) {
			defer openChannels.Done()
			err := s.handleChannel(c)
			if err != nil {
				log.Println(errors.Wrap(err, "failed to handle channel"))
			}
		}(c)
	}
	openChannels.Wait()

	return nil
}

func (s *OpenServerConn) handleChannel(newChannel ssh.NewChannel) error {
	log.Println("NewChannel.ChannelType", newChannel.ChannelType())
	log.Printf("NewChannel.ExtraData: %q\n", newChannel.ExtraData())
	acceptedChannel, reqs, err := newChannel.Accept()
	if err != nil {
		return errors.Wrap(err, "failed to accept")
	}
	var openRequests sync.WaitGroup
	for req := range reqs {
		openRequests.Add(1)
		go func(req *ssh.Request) {
			defer openRequests.Done()
			err := s.handleRequest(acceptedChannel, req)
			if err != nil {
				log.Println(errors.Wrap(err, "failed to handle request"))
			}
		}(req)
	}
	openRequests.Wait()
	return nil
}

func (s *OpenServerConn) handleRequest(chn ssh.Channel, req *ssh.Request) error {
	log.Println("req", req.Type)
	switch req.Type {
	case "exec":
		log.Println("exec received")
		var msg execMsg
		err := ssh.Unmarshal(req.Payload, &msg)
		if err != nil {
			log.Println(errors.Wrap(err, "failed to unmarshal"))
		}
		<-s.connector.AddProducer(msg.Command, chn)
	case "shell":
		log.Println("shell received")
		sessionID := base64.StdEncoding.EncodeToString(s.sshServerConn.SessionID())
		<-s.connector.AddConsumer(sessionID, chn)
	default:
		log.Println("unhandled req.Type", req.Type)
		err := req.Reply(true, nil)
		if err != nil {
			log.Println(errors.Wrap(err, "failed to reply"))
		}
	}
	return nil
}
