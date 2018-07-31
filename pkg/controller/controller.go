/*
Copyright 2018 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"fmt"
	"github.com/golang/glog"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"managed-certs-gke/pkg/config"
	"time"
)

func NewController(opts *config.ControllerOptions) *Controller {
	mcertInformer := opts.McertInformerFactory.Cloud().V1alpha1().ManagedCertificates()

	controller := &Controller{
		Ingress: IngressController{
			client: opts.IngressClient,
			queue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "ingressQueue"),
		},
		Mcert: McertController{
			client: opts.McertClient,
			lister: mcertInformer.Lister(),
			synced: mcertInformer.Informer().HasSynced,
			queue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "mcertQueue"),
			sslClient: opts.SslClient,
			state: newMcertState(),
		},
	}

	mcertInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			controller.Mcert.enqueue(obj)
		},
		UpdateFunc: func(old, new interface{}) {
			controller.Mcert.enqueue(new)
		},
		DeleteFunc: func(obj interface{}) {
			controller.Mcert.enqueue(obj)
		},
	})

	return controller
}

func (c *Controller) Run(stopChannel <-chan struct{}) error {
	defer runtime.HandleCrash()

	glog.Info("Controller.Run()")

	glog.Info("Waiting for managedcertificate cache sync")
	if !cache.WaitForCacheSync(stopChannel, c.Mcert.synced) {
		return fmt.Errorf("Timed out waiting for cache sync")
	}
	glog.Info("Cache synced")

	errors := make(chan error)

	mcertStopChannel := make(chan struct{})
	go c.Mcert.Run(mcertStopChannel, errors)

	ingressStopChannel := make(chan struct{})
	go c.Ingress.Run(ingressStopChannel)

	go wait.Until(c.runIngressWorker, time.Second, stopChannel)

	glog.Info("Waiting for stop signal or error")
	select{
		case <-stopChannel:
			glog.Info("Received stop signal")
			quit(mcertStopChannel, ingressStopChannel)
		case err := <-errors:
			runtime.HandleError(err)
			quit(mcertStopChannel, ingressStopChannel)
	}

	glog.Info("Shutting down")
	return nil
}

func quit(mcertStopChannel, ingressStopChannel chan<- struct{}) {
	mcertStopChannel <- struct{}{}
	ingressStopChannel <- struct{}{}
}
