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
		logrus.Error("Undefined certificate ARN. Please set environment variable CERTIFICATE_CONTROLLER_CERT_ARN")
		os.Exit(1)
	}
	logrus.WithFields(logrus.Fields{"arn": certificateArn}).Info("Certificate ARN found")

	controllerClass := os.Getenv("CERTIFICATE_CONTROLLER_CLASS")
	if controllerClass == "" {
		logrus.WithFields(logrus.Fields{"owner-id": controllerClass}).Error("Undefined owner identifier. Using default")
		controllerClass = "certificate-controller"
	}

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
	if new != nil {
		if context.hasControllingAnnotation(new) {
			context.updateAnnotation(new, context.certificateArn)
		} else {
			logrus.WithFields(logrus.Fields{"action": "skip", "namespace": new.Namespace, "name": new.Name}).Info("No matching controller class on service. Skipping")
		}
		return
	}

	if old != nil && context.hasControllingAnnotation(old) {
		logrus.WithFields(logrus.Fields{"action": "none", "namespace": old.Namespace, "name": old.Name}).Info("Managed service has been externally deleted")
	}
}

func (context *context) updateAnnotation(service *core.Service, annotationValue string) {
	switch service.Annotations[lbCertificateAnnotationKey] {
	case context.certificateArn:
		logrus.WithFields(logrus.Fields{"action": "none", "namespace": service.Namespace, "name": service.Name}).Info("ELB annotation uptodate. No action needed")
		return
	case "":
		logrus.WithFields(logrus.Fields{"action": "add", "namespace": service.Namespace, "name": service.Name}).Info("Adding LoadBalancer annotation")
	default:
		logrus.WithFields(logrus.Fields{"action": "update", "namespace": service.Namespace, "name": service.Name}).Info("Updating LoadBalancer annotation")
	}

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
