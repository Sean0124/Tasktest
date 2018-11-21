package main

import (
	"os/exec"
	"github.com/golang/glog"
)

func delete_yaml(yaml string) (err error)  {
	cmd := exec.Command("kubectl","delete", "-f", yaml)
	glog.Info(cmd)
	err = cmd.Run()
	return err
}


func create_yaml(yaml string) (err error)  {
	cmd := exec.Command("kubectl","create", "-f", yaml)
	glog.Info(cmd)
	err = cmd.Run()
	return err
}
