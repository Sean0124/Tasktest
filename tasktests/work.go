package main

import (
	"os/exec"
	"github.com/golang/glog"
)

func delete_yaml(yaml string) (err error)  {
	cmd := exec.Command("kubectl","delete", "-f", yaml)
	glog.Info(cmd)
	glog.Info(cmd.Run())
	return err
}
