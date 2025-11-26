package main

import (
	"flag"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/abdul-saqib/expose-deployments/controller"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

func main() {
	klog.InitFlags(nil)
	klog.Info("Starting expose-controller...")

	var kubeconfig string
	var masterURL string
	flag.StringVar(&kubeconfig, "kubeconfig", "", "Path to kubeconfig")
	flag.StringVar(&masterURL, "master", "", "API server address")
	flag.Parse()

	var cfg *rest.Config
	var err error

	if kubeconfig != "" {
		klog.Infof("Using kubeconfig: %s", kubeconfig)
		cfg, err = clientcmd.BuildConfigFromFlags(masterURL, filepath.Clean(kubeconfig))
	} else {
		klog.Info("Using InClusterConfig")
		cfg, err = rest.InClusterConfig()
	}
	if err != nil {
		klog.Fatalf("Error building config: %v", err)
	}

	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		klog.Fatalf("Error creating clientset: %v", err)
	}

	klog.Info("Clientset created successfully")

	factory := informers.NewSharedInformerFactory(clientset, 0)
	deployInformer := factory.Apps().V1().Deployments()
	serviceInformer := factory.Core().V1().Services()

	queue := workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "deploy-expose")
	ctrl := controller.NewController(clientset, deployInformer.Lister(), serviceInformer.Lister(), queue)

	klog.Info("Adding event handlers for Deployments")

	_, err = deployInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(obj)
			if err != nil {
				klog.Errorf("Error creating key: %v", err)
				return
			}
			klog.Infof("Add event for key: %s", key)
			ctrl.EnqueueKey(key)
		},
		UpdateFunc: func(_, newObj interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(newObj)
			if err != nil {
				klog.Errorf("Error creating key: %v", err)
				return
			}
			klog.Infof("Update event for key: %s", key)
			ctrl.EnqueueKey(key)
		},
		DeleteFunc: func(obj interface{}) {
			key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
			if err != nil {
				klog.Errorf("Error creating key: %v", err)
				return
			}
			klog.Infof("Delete event for key: %s", key)
			ctrl.EnqueueKey(key)
		},
	})
	if err != nil {
		klog.Fatalf("Error adding event handler: %v", err)
	}

	klog.Info("Starting informer factory...")
	factory.Start(ctrl.StopCh)

	klog.Info("Waiting for caches to sync...")
	if !cache.WaitForCacheSync(ctrl.StopCh, deployInformer.Informer().HasSynced) {
		klog.Fatalf("Cache did not sync")
	}
	klog.Info("Caches synced successfully")

	klog.Info("Starting controller workers...")
	go ctrl.Run(2)

	klog.Info("Controller is running. Waiting for shutdown signal...")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	<-sig

	klog.Info("Shutdown signal received. Stopping controller...")
	close(ctrl.StopCh)
}
