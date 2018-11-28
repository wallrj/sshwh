package server

import (
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

type Server struct {
	config          *Config
	listener        *net.TCPListener
	sshServerConfig *ssh.ServerConfig
	connections     sync.WaitGroup
	quit            chan struct{}
	exited          chan struct{}
}

func (s *Server) bannerCallback(conn ssh.ConnMetadata) string {
	log.Println(conn)
	return "Hello\n"
}

func New(c *Config) (*Server, error) {
	server := &Server{
		config: c,
		quit:   make(chan struct{}),
		exited: make(chan struct{}),
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

func (s *Server) handleConnection(conn net.Conn) error {
	// Before use, a handshake must be performed on the incoming
	// net.Conn.
	_, chans, reqs, err := ssh.NewServerConn(conn, s.sshServerConfig)
	if err != nil {
		return errors.Wrap(err, "failed to handshake")
	}
	go ssh.DiscardRequests(reqs)
	for c := range chans {
		chn, reqs, err := c.Accept()
		if err != nil {
			return errors.Wrap(err, "failed to accept")
		}
		go func() {
			for req := range reqs {
				err := req.Reply(true, nil)
				if err != nil {
					log.Fatal(errors.Wrap(err, "failed to reply"))
				}
			}
		}()
		time.Sleep(time.Second)
		_, err = chn.SendRequest("exit-status", false, []byte{0, 0, 0, 0})
		if err != nil {
			return errors.Wrap(err, "failed to send exit-status")
		}
		_, err = chn.Write([]byte("chn.Write"))
		if err != nil {
			return errors.Wrap(err, "failed to write")
		}
		err = chn.Close()
		if err != nil {
			return errors.Wrap(err, "failed to close")
		}
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
