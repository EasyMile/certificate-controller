package main

import (
	"os"

	"path/filepath"
	"time"

	"github.com/Sirupsen/logrus"
	core "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

const kubeConfig = ".kube/config"
const annotationKey = "service.beta.kubernetes.io/aws-load-balancer-ssl-cert"

var clientset *kubernetes.Clientset
var controller cache.Controller
var certificateArn string
var store cache.Store

func main() {
	certificateArn = os.Getenv("CERTIFICATE_ARN")
	if certificateArn == "" {
		logrus.Error("Undefined certificate ARN. Please set environment variable CERTIFICATE_ARN")
		os.Exit(1)
	}
	logrus.WithFields(logrus.Fields{"arn": certificateArn}).Info("Certificate ARN found")
	var err error
	clientset, err = getClient()
	if err != nil {
		logrus.Error(err)
		os.Exit(1)
	}
	watchServices()
}

func watchServices() {
	watchList := cache.NewListWatchFromClient(clientset.CoreV1().RESTClient(), "services", "",
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
	<-stop
}

func handleServiceAdd(obj interface{}) {
	service := obj.(*core.Service)
	if service.Annotations["easymile.com/certificate"] != "" {
		logrus.WithFields(logrus.Fields{"namespace": service.Namespace, "service": service.Name}).Info("Service added. Adding LoadBalancer annotation")
		updateAnnotation(service, certificateArn)
	}
}
func handleServiceDelete(obj interface{}) {
	service := obj.(*core.Service)
	logrus.WithFields(logrus.Fields{"namespace": service.Namespace, "service": service.Name}).Info("Service deleted. Removing LoadBalancer annotation")
}

func handleServiceUpdate(old, current interface{}) {
	// Cache access example
	serviceInterface, exists, err := store.Get(current)
	if exists && err == nil {
		logrus.WithFields(logrus.Fields{"service": serviceInterface}).Debug("Service found in cache")
	} else {
		logrus.WithFields(logrus.Fields{"err": err}).Warn("Failed to service in cache")
	}

	service := current.(*core.Service)
	oldService := old.(*core.Service)
	if service.Annotations["easymile.com/certificate"] != oldService.Annotations["easymile.com/certificate"] {
		if service.Annotations["easymile.com/certificate"] == "true" {
			logrus.WithFields(logrus.Fields{"namespace": service.Namespace, "service": service.Name}).Info("Adding LoadBalancer annotation")
			updateAnnotation(service, certificateArn)
		} else {
			logrus.WithFields(logrus.Fields{"namespace": service.Namespace, "service": service.Name}).Info("Removing LoadBalancer annotation")
			updateAnnotation(service, "")
		}
	}
}

func updateAnnotation(service *core.Service, annotationValue string) {
	service.Annotations[annotationKey] = annotationValue
	_, err := clientset.CoreV1().Services(service.Namespace).Update(service)
	if err != nil {
		logrus.WithFields(logrus.Fields{"err": err}).Warn("Failed to update the service")
	}
}

func getClient() (*kubernetes.Clientset, error) {
	var config *rest.Config

	KubeConfigPath := filepath.Join(os.Getenv("HOME"), kubeConfig)

	if _, err := os.Stat(KubeConfigPath); os.IsNotExist(err) {
		logrus.Info("Using in cluster config")
		config, err = rest.InClusterConfig()
		if err != nil {
			return nil, err
		}
	} else {
		logrus.WithFields(logrus.Fields{"config": KubeConfigPath}).Info("Using out of cluster config")
		config, err = clientcmd.BuildConfigFromFlags("", KubeConfigPath)
		if err != nil {
			return nil, err
		}
	}
	return kubernetes.NewForConfig(config)
}
