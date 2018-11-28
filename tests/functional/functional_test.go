package functional_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wallrj/sshwh/pkg/client"
	"github.com/wallrj/sshwh/pkg/server"
)

func TestAll(t *testing.T) {
	s, err := server.New(&server.Config{
		Address:     "localhost:2022",
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

	c := &client.Client{
		Address: "localhost:2022",
	}
	err = c.Open()
	require.NoError(t, err)
}
