package main

import "github.com/wallrj/sshwh/pkg/server"

func main() {
	s, err := server.New(&server.Config{
		Address:     ":2022",
		HostKeyPath: "id_rsa",
	})
	if err != nil {
		panic(err)
	}

	err = s.Open()
	if err != nil {
		panic(err)
	}

	err = s.Serve()
	if err != nil {
		panic(err)
	}
}
