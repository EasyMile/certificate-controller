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
const lbCertificateAnnotationKey = "service.beta.kubernetes.io/aws-load-balancer-ssl-cert"
const controllerAnnotationKey = "easymile.com/certificate-controller.class"

type context struct {
	controllerClass string
	certificateArn  string
	clientSet       *kubernetes.Clientset
	store           cache.Store
}

func main() {
	certificateArn := os.Getenv("CERTIFICATE_CONTROLLER_CERT_ARN")
	if certificateArn == "" {
		logrus.Error("Undefined certificate ARN. Please set environment variable CERTIFICATE_ARN")
		os.Exit(1)
	}
	controllerClass := os.Getenv("CERTIFICATE_CONTROLLER_CLASS")
	if controllerClass == "" {
		logrus.WithFields(logrus.Fields{"owner-id": controllerClass}).Error("Undefined owner identifier. Using default")
		controllerClass = "certificate-controller"
	}
	logrus.WithFields(logrus.Fields{"arn": certificateArn}).Info("Certificate ARN found")
	var err error
	clientSet, err := getClient()
	if err != nil {
		logrus.Error(err)
		os.Exit(1)
	}
	context := context{
		certificateArn:  certificateArn,
		controllerClass: controllerClass,
		clientSet:       clientSet,
	}
	watchServices(context)
}

func watchServices(context context) {
	var controller cache.Controller
	watchList := cache.NewListWatchFromClient(context.clientSet.CoreV1().RESTClient(), "services", "",
		fields.Everything())
	context.store, controller = cache.NewInformer(
		watchList,
		&core.Service{},
		time.Second*2,
		cache.ResourceEventHandlerFuncs{
			AddFunc:    context.handleServiceAdd,
			UpdateFunc: context.handleServiceUpdate,
			DeleteFunc: context.handleServiceDelete,
		},
	)

	stop := make(chan struct{})
	go controller.Run(stop)
	<-stop
}

func (context *context) handleServiceAdd(obj interface{}) {
	service := obj.(*core.Service)
	context.reconcile(nil, service)
}
func (context *context) handleServiceDelete(obj interface{}) {
	service := obj.(*core.Service)
	context.reconcile(service, nil)
}

func (context *context) handleServiceUpdate(old, current interface{}) {
	oldService := old.(*core.Service)
	newService := current.(*core.Service)
	if isReconcileNeeded(newService, oldService) {
		context.reconcile(oldService, newService)
	}
}
func (context *context) reconcile(old *core.Service, new *core.Service) {
	if new == nil {
		if old != nil {
			logrus.WithFields(logrus.Fields{"action": "none", "namespace": old.Namespace, "new": old.Name}).Info("Service deleted")
		} else {
			logrus.WithFields(logrus.Fields{"action": "unknown"}).Warn("Unexpected call")
		}
	} else if context.hasControllingAnnotation(new) {
		if new.Annotations[lbCertificateAnnotationKey] == context.certificateArn {
			logrus.WithFields(logrus.Fields{"action": "none", "namespace": new.Namespace, "new": new.Name}).Info("No action needed")
		} else if new.Annotations[lbCertificateAnnotationKey] == "" {
			logrus.WithFields(logrus.Fields{"action": "add", "namespace": new.Namespace, "new": new.Name}).Info("Adding LoadBalancer annotation")
			context.updateAnnotation(new, context.certificateArn)
		} else {
			logrus.WithFields(logrus.Fields{"action": "update", "namespace": new.Namespace, "new": new.Name}).Info("Updating LoadBalancer annotation")
			context.updateAnnotation(new, context.certificateArn)
		}
	} else if old != nil {
		if context.hasControllingAnnotation(old) {
			logrus.WithFields(logrus.Fields{"action": "delete", "namespace": old.Namespace, "new": old.Name}).Info("Deleting LoadBalancer annotation")
			context.updateAnnotation(new, "")
		}
	} else {
		logrus.WithFields(logrus.Fields{"action": "skip", "namespace": new.Namespace, "new": new.Name}).Info("Controller class does not match. Skipping")
	}
}
func (context *context) updateAnnotation(service *core.Service, annotationValue string) {
	service.Annotations[lbCertificateAnnotationKey] = annotationValue
	_, err := context.clientSet.CoreV1().Services(service.Namespace).Update(service)
	if err != nil {
		logrus.WithFields(logrus.Fields{"err": err}).Warn("Failed to update the service")
	}
}

func (context *context) hasControllingAnnotation(new *core.Service) bool {
	return new.Annotations[controllerAnnotationKey] == context.controllerClass
}

func getClient() (*kubernetes.Clientset, error) {
	var config *rest.Config

	kubeConfigPath := filepath.Join(os.Getenv("HOME"), kubeConfig)

	if _, err := os.Stat(kubeConfigPath); os.IsNotExist(err) {
		logrus.Info("Using in cluster config")
		config, err = rest.InClusterConfig()
		if err != nil {
			return nil, err
		}
	} else {
		logrus.WithFields(logrus.Fields{"config": kubeConfigPath}).Info("Using out of cluster config")
		config, err = clientcmd.BuildConfigFromFlags("", kubeConfigPath)
		if err != nil {
			return nil, err
		}
	}
	return kubernetes.NewForConfig(config)
}

func isReconcileNeeded(newService *core.Service, oldService *core.Service) bool {
	return hasAnnotationChanged(lbCertificateAnnotationKey, newService, oldService) ||
		hasAnnotationChanged(controllerAnnotationKey, newService, oldService)
}
func hasAnnotationChanged(annotationKey string, newService *core.Service, oldService *core.Service) bool {
	return newService.Annotations[annotationKey] != oldService.Annotations[annotationKey]
}
