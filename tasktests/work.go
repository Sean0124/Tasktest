package main

import (
	"os/exec"
)

func delete_yaml(yaml string) (err error)  {
	cmd := exec.Command("kubectl","delete", "-f", yaml)
	cmd.Run()
	return err
}
