package controller

import (
	"context"
	"fmt"
	"reflect"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	appslisters "k8s.io/client-go/listers/apps/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

type Controller struct {
	clientset     kubernetes.Interface
	deployLister  appslisters.DeploymentLister
	serviceLister corelisters.ServiceLister
	queue         workqueue.RateLimitingInterface
	StopCh        chan struct{}
}

func NewController(clientset kubernetes.Interface, factory informers.SharedInformerFactory, informer cache.SharedIndexInformer, queue workqueue.RateLimitingInterface) *Controller {
	deployInformer := factory.Apps().V1().Deployments()
	serviceInformer := factory.Core().V1().Services()
	return &Controller{
		clientset:     clientset,
		deployLister:  deployInformer.Lister(),
		serviceLister: serviceInformer.Lister(),
		queue:         queue,
		StopCh:        make(chan struct{}),
	}
}

func (c *Controller) EnqueueKey(key string) {
	c.queue.Add(key)
}

func (c *Controller) Run(workers int) {
	for range workers {
		go wait.Until(c.worker, time.Second*30, c.StopCh)
	}
	<-c.StopCh
}

func (c *Controller) worker() {
	for c.processItem() {
	}
}

func (c *Controller) processItem() bool {
	obj, shutdown := c.queue.Get()
	if shutdown {
		return false
	}

	key, ok := obj.(string)
	if !ok {
		klog.Errorf("Expected string key in queue but got: %T", obj)
		c.queue.Done(obj)
		return true
	}

	klog.Infof("Processing key: %s", key)
	err := c.syncHandler(key)
	c.queue.Done(obj)

	if err != nil {
		klog.Errorf("Error syncing %s: %v", key, err)
		c.queue.AddRateLimited(key)
		return true
	}

	return true
}

func (c *Controller) syncHandler(key string) error {
	klog.Infof("syncHandler: processing key=%s", key)

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return fmt.Errorf("invalid resource key %s: %v", key, err)
	}

	svcName := name + "-expose"
	deploy, err := c.deployLister.Deployments(namespace).Get(name)
	if err != nil {
		if errors.IsNotFound(err) {
			klog.Infof("Deployment %s/%s deleted, cleaning up service %s", namespace, name, svcName)
			return c.removeService(namespace, svcName)
		}
		return fmt.Errorf("failed to get deployment %s/%s: %v", namespace, name, err)
	}

	klog.Infof("syncHandler: deployment %s/%s exists, reconciling service...", namespace, name)

	svc, err := c.serviceLister.Services(namespace).Get(svcName)
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to get service %s/%s: %v", namespace, svcName, err)
	}

	selector := deploy.Spec.Template.Labels
	if len(selector) == 0 {
		klog.Warningf("Deployment %s/%s has no labels, cannot create service", namespace, name)
		return nil
	}

	desired := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcName,
			Namespace: namespace,
		},
		Spec: v1.ServiceSpec{
			Type:     v1.ServiceTypeNodePort,
			Selector: selector,
			Ports: []v1.ServicePort{
				{
					Name:       "http",
					Port:       80,
					TargetPort: intstr.FromInt(80),
				},
			},
		},
	}

	if errors.IsNotFound(err) {
		return c.createService(desired, namespace, svcName)
	}

	if !reflect.DeepEqual(svc.Spec.Selector, desired.Spec.Selector) ||
		!reflect.DeepEqual(svc.Spec.Ports, desired.Spec.Ports) {

		klog.Infof("Service %s/%s requires update", namespace, svcName)
		return c.updateService(svc, desired, namespace, svcName)
	}

	klog.Infof("Reconciliation of %s/%s completed successfully", namespace, name)
	return nil
}

func (c *Controller) createService(desired *v1.Service, namespace, svcName string) error {
	klog.Infof("Service %s/%s missing, creating...", namespace, svcName)
	_, err := c.clientset.CoreV1().Services(namespace).Create(
		context.Background(),
		desired,
		metav1.CreateOptions{},
	)
	if err != nil {
		return fmt.Errorf("failed to create service %s/%s: %v", namespace, svcName, err)
	}
	klog.Infof("Service %s/%s created", namespace, svcName)
	return nil
}

func (c *Controller) updateService(svc, desired *v1.Service, namespace, svcName string) error {
	updated := svc.DeepCopy()
	updated.Spec.Selector = desired.Spec.Selector
	updated.Spec.Ports = desired.Spec.Ports

	_, err := c.clientset.CoreV1().Services(namespace).Update(
		context.Background(),
		updated,
		metav1.UpdateOptions{},
	)
	if err != nil {
		return fmt.Errorf("failed to update service %s/%s: %v", namespace, svcName, err)
	}

	klog.Infof("Service %s/%s updated", namespace, svcName)
	return nil
}

func (c *Controller) removeService(namespace, svcName string) error {
	delErr := c.clientset.CoreV1().Services(namespace).Delete(
		context.Background(),
		svcName,
		metav1.DeleteOptions{},
	)
	if delErr != nil && !errors.IsNotFound(delErr) {
		return fmt.Errorf("failed to delete service %s/%s: %v", namespace, svcName, delErr)
	}

	klog.Infof("Service %s/%s deleted (if existed)", namespace, svcName)
	return nil
}
