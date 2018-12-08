package functional_test

import (
	"bytes"
	"log"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wallrj/sshwh/pkg/client"
	"github.com/wallrj/sshwh/pkg/server"
)

func TestAll(t *testing.T) {
	s, err := server.New(&server.Config{
		Address:     "127.0.0.1:2022",
		HostKeyPath: "testdata/id_rsa",
	})
	require.NoError(t, err)
	err = s.Open()
	require.NoError(t, err)
	go func() {
		err := s.Serve()
		require.NoError(t, err)
	}()
	defer s.Close()

	receiver := client.New(&client.Config{
		Address: "127.0.0.1:2022",
	})
	receiverSessionID, err := receiver.Open()
	require.NoError(t, err)
	defer func() {
		err := receiver.Close()
		require.NoError(t, err)
	}()
	time.Sleep(time.Second)

	received := &bytes.Buffer{}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer log.Println("receive end")
		log.Println("receive start")
		err := receiver.Receive(received)
		require.NoError(t, err)
	}()
	time.Sleep(time.Second)

	sender := client.New(&client.Config{
		Address: "127.0.0.1:2022",
	})
	_, err = sender.Open()
	require.NoError(t, err)
	defer func() {
		err := sender.Close()
		require.NoError(t, err)
	}()
	time.Sleep(time.Second)

	expected := "test1234"
	sent := bytes.NewBufferString(expected)
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer log.Println("send end")
		log.Println("send start")
		err := sender.Send(receiverSessionID, sent)
		require.NoError(t, err)
	}()
	time.Sleep(time.Second)
	log.Println("waiting for clients to finish")
	wg.Wait()
	assert.Equal(t, expected, received.String())
}
