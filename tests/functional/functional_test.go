package functional_test

import (
	"bytes"
	"sync"
	"testing"

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
	received := &bytes.Buffer{}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := receiver.Receive(received)
		require.NoError(t, err)
	}()

	sender := client.New(&client.Config{
		Address: "127.0.0.1:2022",
	})

	expected := "test1234"
	sent := bytes.NewBufferString(expected)
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := sender.Send(sent)
		require.NoError(t, err)
	}()
	wg.Wait()
	assert.Equal(t, expected, received.String())
}
