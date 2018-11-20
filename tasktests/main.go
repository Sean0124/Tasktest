package main

import (
	"flag"
	"time"

	"github.com/golang/glog"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	// Uncomment the following line to load the gcp plugin (only required to authenticate against GKE clusters).
	// _ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	clientset "github.com/sean/tasktests/pkg/client/clientset/versioned"
	informers "github.com/sean/tasktests/pkg/client/informers/externalversions"
	"github.com/sean/tasktests/pkg/signals"
)

var (
	masterURL  string
	kubeconfig string
)

func main() {
	flag.Parse()

	// set up signals so we handle the first shutdown signal gracefully
	stopCh := signals.SetupSignalHandler()

	//根据配置信息,生成cfg,用于访问kube-api的配置信息
	cfg, err := clientcmd.BuildConfigFromFlags(masterURL, kubeconfig)
	if err != nil {
		glog.Fatalf("Error building kubeconfig: %s", err.Error())
	}

	//创建一个kubernetes的client
	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		glog.Fatalf("Error building kubernetes clientset: %s", err.Error())
	}

	//创建一个tasktest对象的client
	tasktestClient, err := clientset.NewForConfig(cfg)
	if err != nil {
		glog.Fatalf("Error building example clientset: %s", err.Error())
	}

	//为tasktest对象创建一个叫作InformerFactory的工厂,并使用它生成一个tasktest对象的Informer
	tasktestInformerFactory := informers.NewSharedInformerFactory(tasktestClient, time.Second*30)

	//将tasktest对象的Informer,传递给控制器
	controller := NewController(kubeClient, tasktestClient,
		tasktestInformerFactory.Cloudclusters().V1().TaskTests())

	//启动生成的Informer
	go tasktestInformerFactory.Start(stopCh)

	//执行controller.Run,启动自定义控制器
	if err = controller.Run(2, stopCh); err != nil {
		glog.Fatalf("Error running controller: %s", err.Error())
	}
}

func init() {
	flag.StringVar(&kubeconfig, "kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	flag.StringVar(&masterURL, "master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
}
