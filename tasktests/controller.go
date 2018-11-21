package main

import (
	"fmt"
	"time"

	"github.com/golang/glog"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	cloudclustersv1 "github.com/sean/tasktests/pkg/apis/cloudclusters/v1"
	clientset "github.com/sean/tasktests/pkg/client/clientset/versioned"
	tasktestscheme "github.com/sean/tasktests/pkg/client/clientset/versioned/scheme"
	informers "github.com/sean/tasktests/pkg/client/informers/externalversions/cloudclusters/v1"
	listers "github.com/sean/tasktests/pkg/client/listers/cloudclusters/v1"
)

const controllerAgentName = "tasktest-controller"

const (
	// SuccessSynced is used as part of the Event 'reason' when a TaskTest is synced
	SuccessSynced = "Synced"

	// MessageResourceSynced is the message used for an Event fired when a TaskTest
	// is synced successfully
	MessageResourceSynced = "TaskTest synced successfully"
)

// Controller is the controller implementation for TaskTest resources
type Controller struct {
	// kubeclientset is a standard kubernetes clientset
	kubeclientset kubernetes.Interface
	// tasktestclientset is a clientset for our own API group
	tasktestclientset clientset.Interface

	tasktestsLister listers.TaskTestLister
	tasktestsSynced cache.InformerSynced

	// workqueue is a rate limited work queue. This is used to queue work to be
	// processed instead of performing it as soon as a change happens. This
	// means we can ensure we only process a fixed amount of resources at a
	// time, and makes it easy to ensure we are never processing the same item
	// simultaneously in two different workers.
	workqueue workqueue.RateLimitingInterface
	// recorder is an event recorder for recording Event resources to the
	// Kubernetes API.
	recorder record.EventRecorder
}

// NewController returns a new tasktest controller
func NewController(
	kubeclientset kubernetes.Interface,
	tasktestclientset clientset.Interface,
	tasktestInformer informers.TaskTestInformer) *Controller {

	// Create event broadcaster
	// Add sample-controller types to the default Kubernetes Scheme so Events can be
	// logged for sample-controller types.
	utilruntime.Must(tasktestscheme.AddToScheme(scheme.Scheme))
	glog.V(4).Info("Creating event broadcaster")
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: kubeclientset.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: controllerAgentName})

	controller := &Controller{
		kubeclientset:    kubeclientset,
		tasktestclientset: tasktestclientset,
		tasktestsLister:   tasktestInformer.Lister(),
		tasktestsSynced:   tasktestInformer.Informer().HasSynced,
		workqueue:        workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "TaskTests"),
		recorder:         recorder,
	}

	glog.Info("Setting up event handlers")
	// Set up an event handler for when TaskTest resources change
	tasktestInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueTaskTest,
		UpdateFunc: func(old, new interface{}) {
			oldTaskTest := old.(*cloudclustersv1.TaskTest)
			newTaskTest := new.(*cloudclustersv1.TaskTest)
			if oldTaskTest.Status.TaskStatus == "successed" {
				glog.Info("%s/%s alreadly successed... ", oldTaskTest.Namespace,oldTaskTest.Name)
				return
			}
			if oldTaskTest.ResourceVersion == newTaskTest.ResourceVersion {
				// Periodic resync will send update events for all known TaskTests.
				// Two different versions of the same TaskTest will always have different RVs.
				return
			}
			controller.enqueueTaskTest(new)
		},
		DeleteFunc: controller.enqueueTaskTestForDelete,
	})

	return controller
}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) error {
	defer runtime.HandleCrash()
	defer c.workqueue.ShutDown()

	// Start the informer factories to begin populating the informer caches
	glog.Info("Starting TaskTest control loop")

	// Wait for the caches to be synced before starting workers
	glog.Info("Waiting for informer caches to sync")
	if ok := cache.WaitForCacheSync(stopCh, c.tasktestsSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	glog.Info("Starting workers")
	// Launch two workers to process TaskTest resources
	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	glog.Info("Started workers")
	<-stopCh
	glog.Info("Shutting down workers")

	return nil
}

// runWorker is a long-running function that will continually call the
// processNextWorkItem function in order to read and process a message on the
// workqueue.
func (c *Controller) runWorker() {
	for c.processNextWorkItem() {
	}
}

// processNextWorkItem will read a single work item off the workqueue and
// attempt to process it, by calling the syncHandler.
func (c *Controller) processNextWorkItem() bool {
	obj, shutdown := c.workqueue.Get()

	if shutdown {
		return false
	}

	// We wrap this block in a func so we can defer c.workqueue.Done.
	err := func(obj interface{}) error {
		// We call Done here so the workqueue knows we have finished
		// processing this item. We also must remember to call Forget if we
		// do not want this work item being re-queued. For example, we do
		// not call Forget if a transient error occurs, instead the item is
		// put back on the workqueue and attempted again after a back-off
		// period.
		defer c.workqueue.Done(obj)
		var key string
		var ok bool
		// We expect strings to come off the workqueue. These are of the
		// form namespace/name. We do this as the delayed nature of the
		// workqueue means the items in the informer cache may actually be
		// more up to date that when the item was initially put onto the
		// workqueue.
		if key, ok = obj.(string); !ok {
			// As the item in the workqueue is actually invalid, we call
			// Forget here else we'd go into a loop of attempting to
			// process a work item that is invalid.
			c.workqueue.Forget(obj)
			runtime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		// Run the syncHandler, passing it the namespace/name string of the
		// TaskTest resource to be synced.
		if err := c.syncHandler(key); err != nil {
			return fmt.Errorf("error syncing '%s': %s", key, err.Error())
		}
		// Finally, if no error occurs we Forget this item so it does not
		// get queued again until another change happens.
		c.workqueue.Forget(obj)
		glog.Infof("Successfully synced '%s'", key)
		return nil
	}(obj)

	if err != nil {
		runtime.HandleError(err)
		return true
	}

	return true
}

// syncHandler compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the TaskTest resource
// with the current status of the resource.
func (c *Controller) syncHandler(key string) error {
	// Convert the namespace/name string into a distinct namespace and name
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	// Get the TaskTest resource with this namespace/name
	tasktest, err := c.tasktestsLister.TaskTests(namespace).Get(name)
	if err != nil {
		//判断task是否已经运行成功,成功则跳过
		if tasktest.Status.TaskStatus == "successed" {
			glog.Info("tasktest: %s/%s already successed ..", namespace, name)
			return nil
		}

		// The TaskTest resource may no longer exist, in which case we stop
		// processing.
		if errors.IsNotFound(err) {
			glog.Warningf("TaskTest: %s/%s does not exist in local cache, will delete it from Neutron ...",
				namespace, name)

			glog.Infof("[Neutron] Deleting tasktest: %s/%s ...", namespace, name)

			// FIX ME: call Neutron API to delete this tasktest by name.
			//
			// neutron.Delete(namespace, name)

			return nil
		}

		runtime.HandleError(fmt.Errorf("failed to list tasktest by: %s/%s", namespace, name))

		return err
	}

	glog.Infof("[Neutron] Try to process tasktest: %#v ...", tasktest)

	if tasktest.Spec.TaskTest == "backup" {
		glog.Info("Try to process backup...")
	} else if tasktest.Spec.TaskTest == "delete" {
		glog.Info("Try to process delete...")
		err = delete_yaml(tasktest.Spec.Yaml)
		if err != nil {
			glog.Info(err)
		}
	} else if tasktest.Spec.TaskTest == "deploy" {
		glog.Info("Try to process deloy...")
		err = create_yaml(tasktest.Spec.Yaml)
		if err != nil {
			glog.Info(err)
		}
	}

	// FIX ME: Do diff().
	//
	// actualTaskTest, exists := neutron.Get(namespace, name)
	//
	// if !exists {
	// 	neutron.Create(namespace, name)
	// } else if !reflect.DeepEqual(actualTaskTest, tasktest) {
	// 	neutron.Update(namespace, name)
	// }

	// Finally, we update the status block of the Foo resource to reflect the
	// current state of the world
	err = c.updateTaskTestStatus(tasktest)
	if err != nil {
		return err
	}

	c.recorder.Event(tasktest, corev1.EventTypeNormal, SuccessSynced, MessageResourceSynced)
	return nil
}

func (c *Controller) updateTaskTestStatus(tasktest *cloudclustersv1.TaskTest) error {
	// NEVER modify objects from the store. It's a read-only, local cache.
	// You can use DeepCopy() to make a deep copy of original object and modify this copy
	// Or create a copy manually for better performance
	tasktestCopy := tasktest.DeepCopy()
	tasktestCopy.Status.TaskStatus = "successed"
	// If the CustomResourceSubresources feature gate is not enabled,
	// we must use Update instead of UpdateStatus to update the Status block of the Foo resource.
	// UpdateStatus will not allow changes to the Spec of the resource,
	// which is ideal for ensuring nothing other than resource status has been updated.
	//_, err := c.sampleclientset.SamplecontrollerV1alpha1().Foos(foo.Namespace).Update(fooCopy)
	_, err := c.tasktestclientset.CloudclustersV1().TaskTests(tasktest.Namespace).Update(tasktestCopy)
	return err
}

// enqueueTaskTest takes a TaskTest resource and converts it into a namespace/name
// string which is then put onto the work queue. This method should *not* be
// passed resources of any type other than TaskTest.
func (c *Controller) enqueueTaskTest(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		runtime.HandleError(err)
		return
	}
	c.workqueue.AddRateLimited(key)
}

// enqueueTaskTestForDelete takes a deleted TaskTest resource and converts it into a namespace/name
// string which is then put onto the work queue. This method should *not* be
// passed resources of any type other than TaskTest.
func (c *Controller) enqueueTaskTestForDelete(obj interface{}) {
	var key string
	var err error
	key, err = cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		runtime.HandleError(err)
		return
	}
	c.workqueue.AddRateLimited(key)
}
