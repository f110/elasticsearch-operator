package elasticsearchindextemplate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	databasev1alpha1 "github.com/f110/elasticsearch-operator/pkg/apis/database/v1alpha1"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("controller_elasticsearchindextemplate")

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

// Add creates a new ElasticsearchIndexTemplate Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileElasticsearchIndexTemplate{client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("elasticsearchindextemplate-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource ElasticsearchIndexTemplate
	err = c.Watch(&source.Kind{Type: &databasev1alpha1.ElasticsearchIndexTemplate{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// TODO(user): Modify this to be the types you create that are owned by the primary resource
	// Watch for changes to secondary resource Pods and requeue the owner ElasticsearchIndexTemplate
	err = c.Watch(&source.Kind{Type: &corev1.Pod{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &databasev1alpha1.ElasticsearchIndexTemplate{},
	})
	if err != nil {
		return err
	}

	return nil
}

var _ reconcile.Reconciler = &ReconcileElasticsearchIndexTemplate{}

// ReconcileElasticsearchIndexTemplate reconciles a ElasticsearchIndexTemplate object
type ReconcileElasticsearchIndexTemplate struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for a ElasticsearchIndexTemplate object and makes changes based on the state read
// and what is in the ElasticsearchIndexTemplate.Spec
// TODO(user): Modify this Reconcile function to implement your Controller logic.  This example creates
// a Pod as an example
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileElasticsearchIndexTemplate) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling ElasticsearchIndexTemplate")

	// Fetch the ElasticsearchIndexTemplate instance
	instance := &databasev1alpha1.ElasticsearchIndexTemplate{}
	err := r.client.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	clusters := &databasev1alpha1.ElasticsearchClusterList{}
	err = r.client.List(context.TODO(), client.ListOptions{}.MatchingLabels(instance.Spec.Selector.MatchLabels), clusters)
	if err != nil {
		return reconcile.Result{}, nil
	}

	jobs := newJobsForCR(instance, clusters)

	for _, job := range jobs {
		// Set ElasticsearchIndexTemplate instance as the owner and controller
		if err := controllerutil.SetControllerReference(instance, job, r.scheme); err != nil {
			return reconcile.Result{}, err
		}

		// Check if this Pod already exists
		found := &corev1.Pod{}
		err = r.client.Get(context.TODO(), types.NamespacedName{Name: job.Name, Namespace: job.Namespace}, found)
		if err != nil && errors.IsNotFound(err) {
			reqLogger.Info("Creating a new Job", "Job.Namespace", job.Namespace, "Job.Name", job.Name)
			err = r.client.Create(context.TODO(), job)
			if err != nil {
				return reconcile.Result{}, err
			}
		} else if err != nil {
			return reconcile.Result{}, err
		}
	}

	return reconcile.Result{}, nil
}

func newJobsForCR(cr *databasev1alpha1.ElasticsearchIndexTemplate, clusters *databasev1alpha1.ElasticsearchClusterList) []*batchv1.Job {
	s := sha256.Sum256([]byte(cr.Spec.IndexTemplate))
	templateHash := hex.EncodeToString(s[:])

	jobs := make([]*batchv1.Job, 0)
	for _, c := range clusters.Items {
		jobs = append(jobs, &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      c.Name + "-index-template-" + templateHash[:9],
				Namespace: c.Namespace,
			},
			Spec: batchv1.JobSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						InitContainers: []corev1.Container{
							{
								Name:            "wait-for-service",
								Image:           "busybox",
								ImagePullPolicy: corev1.PullIfNotPresent,
								Command:         []string{"sh", "-c", "until wget -q http://" + c.Name + "client:9200/_cluster/health?local; do echo waiting for client service; sleep 2; done;"},
							},
						},
						Containers: []corev1.Container{
							{
								Name:            "create-index-template",
								Image:           "elasticsearch:6.6.0",
								ImagePullPolicy: corev1.PullIfNotPresent,
								Command: []string{
									"/bin/bash",
									"-c",
									"set -e; for file in `ls /templates`; do curl --fail -s -XPUT -H \"Content-Type: application/json\" -d '" + cr.Spec.IndexTemplate + "' http://" + c.Name + "-client:9200/_template/$(basename \"$file\" .json); done;",
								},
							},
						},
					},
				},
			},
		})
	}

	return jobs
}
