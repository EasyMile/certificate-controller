package main

import (
	"os"

	"time"

	"github.com/Sirupsen/logrus"
	"github.com/urfave/cli"
	core "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

var VERSION = "v0.0.0-dev"

var clientset *kubernetes.Clientset

var controller cache.Controller
var store cache.Store
var imageCapacity map[string]int64

func main() {
	app := cli.NewApp()
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:  "config",
			Usage: "Kube config path for outside of cluster access",
		},
	}

	app.Action = func(c *cli.Context) error {
		var err error
		clientset, err = getClient(c.String("config"))
		if err != nil {
			logrus.Error(err)
			return err
		}
		// go pollServices()
		watchServices()
		for {
			time.Sleep(5 * time.Second)
		}
	}
	app.Run(os.Args)
}

func watchServices() {
	//Regular informer
	watchList := cache.NewListWatchFromClient(clientset.Core().RESTClient(), "services", "",
		fields.Everything())
	store, controller = cache.NewInformer(
		watchList,
		&core.Service{},
		time.Second*2,
		cache.ResourceEventHandlerFuncs{
			AddFunc:    handleServiceAdd,
			UpdateFunc: handleServiceUpdate,
			DeleteFunc: handleServiceDelete,
		},
	)

	stop := make(chan struct{})
	go controller.Run(stop)
}

func handleServiceAdd(obj interface{}) {
	service := obj.(*core.Service)
	if service.Annotations["easymile.com/certificate"] != "" {
		logrus.Infof("Service [%s/%s] is added. Add LoadBalancer annotation", service.Namespace, service.Name)
		service.Annotations["service.beta.kubernetes.io/aws-load-balancer-ssl-cert"] = "arn:aws:acm:eu-west-2:660006099962:certificate/9e3eed1a-1b23-4e3d-ab2f-c316509433ac"
		_, err := clientset.Core().Services(service.Namespace).Update(service)
		if err != nil {
			logrus.Warnf("Failed to update the service: %v", err)
		}
	}
}

func handleServiceDelete(obj interface{}) {
	service := obj.(*core.Service)
	logrus.Infof("Service [%s/%s] is deleted.", service.Namespace, service.Name)
}

func handleServiceUpdate(old, current interface{}) {
	// Cache access example
	serviceInterface, exists, err := store.Get(current)
	if exists && err == nil {
		logrus.Debugf("Found the service [%v] in cache", serviceInterface)
	} else {
		logrus.Warnf("Error finding in cache: %s", err)
	}

	service := current.(*core.Service)
	old_service := old.(*core.Service)
	if service.Annotations["easymile.com/certificate"] != old_service.Annotations["easymile.com/certificate"] {
		if service.Annotations["easymile.com/certificate"] == "true" {
			logrus.Infof("Service [%s/%s] is updated. Adding LoadBalancer annotation", service.Namespace, service.Name)
			service.Annotations["service.beta.kubernetes.io/aws-load-balancer-ssl-cert"] = "arn:aws:acm:eu-west-2:660006099962:certificate/9e3eed1a-1b23-4e3d-ab2f-c316509433ac"
		} else {
			logrus.Infof("Service [%s/%s] is updated. Removing LoadBalancer annotation", service.Namespace, service.Name)
			service.Annotations["service.beta.kubernetes.io/aws-load-balancer-ssl-cert"] = ""
		}
		_, err := clientset.Core().Services(service.Namespace).Update(service)
		if err != nil {
			logrus.Warnf("Failed to update the service: %v", err)
		}
	}
}

func getClient(pathToCfg string) (*kubernetes.Clientset, error) {
	var config *rest.Config
	var err error
	if pathToCfg == "" {
		logrus.Info("Using in cluster config")
		config, err = rest.InClusterConfig()
		// in cluster access
	} else {
		logrus.Info("Using out of cluster config")
		config, err = clientcmd.BuildConfigFromFlags("", pathToCfg)
	}
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(config)
}
