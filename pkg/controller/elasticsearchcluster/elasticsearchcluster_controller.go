package elasticsearchcluster

import (
	"context"

	databasev1alpha1 "github.com/f110/elasticsearch-operator/pkg/apis/database/v1alpha1"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("controller_elasticsearchcluster")

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

// Add creates a new ElasticsearchCluster Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileElasticsearchCluster{client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("elasticsearchcluster-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource ElasticsearchCluster
	err = c.Watch(&source.Kind{Type: &databasev1alpha1.ElasticsearchCluster{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	err = c.Watch(&source.Kind{Type: &appsv1.StatefulSet{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &databasev1alpha1.ElasticsearchCluster{},
	})
	if err != nil {
		return err
	}

	return nil
}

var _ reconcile.Reconciler = &ReconcileElasticsearchCluster{}

// ReconcileElasticsearchCluster reconciles a ElasticsearchCluster object
type ReconcileElasticsearchCluster struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for a ElasticsearchCluster object and makes changes based on the state read
// and what is in the ElasticsearchCluster.Spec
// TODO(user): Modify this Reconcile function to implement your Controller logic.  This example creates
// a Pod as an example
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileElasticsearchCluster) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling ElasticsearchCluster")

	// Fetch the ElasticsearchCluster instance
	instance := &databasev1alpha1.ElasticsearchCluster{}
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

	if instance.Spec.HotWarm {
		reqLogger.Info("Reconciling as HotWarm Cluster")
		return r.hotWarmClusterReconcile(reqLogger, instance)
	}

	return reconcile.Result{}, nil
}

func (r *ReconcileElasticsearchCluster) hotWarmClusterReconcile(reqLogger logr.Logger, instance *databasev1alpha1.ElasticsearchCluster) (reconcile.Result, error) {
	masterStatefulset := newMasterStatefulsetForCR(instance)
	if res, err := r.statefulNodeReconcile(reqLogger, instance, masterStatefulset); err != nil {
		return res, err
	}
	hotStatefulset := newHotStatefulsetForCR(instance)
	if res, err := r.statefulNodeReconcile(reqLogger, instance, hotStatefulset); err != nil {
		return res, err
	}
	warmStatefulSet := newWarmStatefulsetForCR(instance)
	if res, err := r.statefulNodeReconcile(reqLogger, instance, warmStatefulSet); err != nil {
		return res, err
	}
	clientDeployment := newClientDeploymentForCR(instance)
	if res, err := r.deploymentReconcile(reqLogger, instance, clientDeployment); err != nil {
		return res, err
	}

	return reconcile.Result{}, nil
}

func (r *ReconcileElasticsearchCluster) deploymentReconcile(reqLogger logr.Logger, instance *databasev1alpha1.ElasticsearchCluster, deployment *appsv1.Deployment) (reconcile.Result, error) {
	if err := controllerutil.SetControllerReference(instance, deployment, r.scheme); err != nil {
		return reconcile.Result{}, err
	}

	found := &appsv1.Deployment{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: deployment.Name, Namespace: deployment.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating a new Deployment", "Deployment.Namespace", deployment.Namespace, "Deployment.Name", deployment.Name)
		err = r.client.Create(context.TODO(), deployment)
		if err != nil {
			return reconcile.Result{}, err
		}

		return reconcile.Result{}, nil
	} else if err != nil {
		return reconcile.Result{}, err
	}

	reqLogger.Info("Updating exists Deployment", "Deployment.Namespace", found.Namespace, "Deployment.Name", found.Name)
	if err := r.client.Update(context.TODO(), deployment); err != nil {
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}

func (r *ReconcileElasticsearchCluster) statefulNodeReconcile(reqLogger logr.Logger, instance *databasev1alpha1.ElasticsearchCluster, node *appsv1.StatefulSet) (reconcile.Result, error) {
	if err := controllerutil.SetControllerReference(instance, node, r.scheme); err != nil {
		return reconcile.Result{}, err
	}

	found := &appsv1.StatefulSet{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: node.Name, Namespace: node.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating a new StatefulSet", "StatefulSet.Namespace", node.Namespace, "StatefulSet.Name", node.Name)
		err = r.client.Create(context.TODO(), node)
		if err != nil {
			return reconcile.Result{}, err
		}

		return reconcile.Result{}, nil
	} else if err != nil {
		return reconcile.Result{}, err
	}

	reqLogger.Info("Updating exists StatefulSet", "StatefulSet.Namespace", found.Namespace, "StatefulSet.Name", found.Name)
	if err := r.client.Update(context.TODO(), node); err != nil {
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}

func newHotStatefulsetForCR(cr *databasev1alpha1.ElasticsearchCluster) *appsv1.StatefulSet {
	return newStatefulsetNode(cr.Name, cr.Spec.HotNode)
}

func newWarmStatefulsetForCR(cr *databasev1alpha1.ElasticsearchCluster) *appsv1.StatefulSet {
	return newStatefulsetNode(cr.Name, cr.Spec.HotNode)
}

func newMasterStatefulsetForCR(cr *databasev1alpha1.ElasticsearchCluster) *appsv1.StatefulSet {
	return newStatefulsetNode(cr.Name, cr.Spec.MasterNode)
}

func newClientDeploymentForCR(cr *databasev1alpha1.ElasticsearchCluster) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: cr.Name + "-client-node",
			Labels: map[string]string{
				"role": "client",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &cr.Spec.ClientNode.Count,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"role": "client",
					},
				},
				Spec: corev1.PodSpec{
					InitContainers: initContainersForElasticsearch("data"),
					Containers:     []corev1.Container{},
					SecurityContext: &corev1.PodSecurityContext{
						FSGroup: Int64(1000),
					},
					Volumes: []corev1.Volume{
						{
							Name: "conf",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: cr.Name + "-client-es-conf",
									},
								},
							},
						},
						{
							Name: "log4j2-conf",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: cr.Name + "-log4j2-conf",
									},
								},
							},
						},
						{
							Name: "data",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{
									Medium:    corev1.StorageMediumMemory,
									SizeLimit: resource.NewQuantity(256*1024*1024, resource.BinarySI), // 256Mi
								},
							},
						},
					},
				},
			},
		},
	}
}

func newStatefulsetNode(name string, spec databasev1alpha1.ElasticsearchClusterNodeSpec) *appsv1.StatefulSet {
	dataVolumeName := name + "-hot-data"

	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: name + "-hot-node",
			Labels: map[string]string{
				"app": "elasticsearch-cluster",
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &spec.Count,
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: dataVolumeName,
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								"storage": resource.MustParse(spec.DiskSize + "Gi"),
							},
						},
					},
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"role": "hot",
					},
				},
				Spec: corev1.PodSpec{
					InitContainers: initContainersForElasticsearch(dataVolumeName),
					Containers: []corev1.Container{
						{
							Name:            "elasticsearch",
							Image:           "elasticsearch/elasticsearch:v6.6.0",
							ImagePullPolicy: corev1.PullIfNotPresent,
							Env: []corev1.EnvVar{
								{
									Name: "PROCESSORS",
									ValueFrom: &corev1.EnvVarSource{
										ResourceFieldRef: &corev1.ResourceFieldSelector{
											Resource: "limits.cpu",
										},
									},
								},
								{
									Name:  "ES_JAVA_OPTS",
									Value: "-Djava.net.preferIPv4Stack=true -Xms{{ .Values.hot.heapSize }} -Xmx{{ .Values.hot.heapSize }}",
								},
							},
							Ports: []corev1.ContainerPort{
								{ContainerPort: 9300},
								{ContainerPort: 9200},
							},
							ReadinessProbe: &corev1.Probe{
								InitialDelaySeconds: 5,
								Handler: corev1.Handler{
									HTTPGet: &corev1.HTTPGetAction{
										Port: intstr.FromInt(9200),
										Path: "/_cluster/health?local=true",
									},
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      dataVolumeName,
									MountPath: "/usr/share/elasticsearch/data",
								},
								{
									Name:      "conf",
									MountPath: "/usr/share/elasticsearch/config/elasticsearch.yml",
									SubPath:   "elasticsearch.yml",
								},
								{
									Name:      "log4j2-conf",
									MountPath: "/usr/share/elasticsearch/config/log4j2.properties",
									SubPath:   "log4j2.properties",
								},
							},
						},
					},
					SecurityContext: &corev1.PodSecurityContext{
						FSGroup: Int64(1000),
					},
					Volumes: []corev1.Volume{
						{
							Name: "conf",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: name + "-hot-es-conf",
									},
								},
							},
						},
						{
							Name: "log4j2-conf",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: name + "-log4j2-conf",
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func initContainersForElasticsearch(dataVolumeName string) []corev1.Container {
	return []corev1.Container{
		{
			Name:            "sysctl",
			Image:           "elasticsearch/elasticsearch:v6.6.0",
			ImagePullPolicy: corev1.PullIfNotPresent,
			Command:         []string{"sysctl", "-w", "vm.max_map_count=262144"},
			SecurityContext: &corev1.SecurityContext{
				Privileged: Bool(true),
			},
		},
		{
			Name:            "chown",
			Image:           "elasticsearch/elasticsearch:v6.6.0",
			ImagePullPolicy: corev1.PullIfNotPresent,
			Command: []string{"/bin/bash", "-c", `set -e;
              set -x;
              chown elasticsearch:elasticsearch /usr/share/elasticsearch/data;
              for datadir in $(find /usr/share/elasticsearch/data -mindepth 1 -maxdepth 1 -not -name ".snapshot"); do
                chown -R elasticsearch:elasticsearch $datadir;
              done;
              chown elasticsearch:elasticsearch /usr/share/elasticsearch/logs;
              for logfile in $(find /usr/share/elasticsearch/logs -mindepth 1 -maxdepth 1 -not -name ".snapshot"); do
                chown -R elasticsearch:elasticsearch $logfile;
              done`},
			SecurityContext: &corev1.SecurityContext{
				RunAsUser: Int64(0),
			},
			VolumeMounts: []corev1.VolumeMount{
				{
					Name:      dataVolumeName,
					MountPath: "/usr/share/elasticsearch/data",
				},
			},
		},
	}
}

func Bool(b bool) *bool {
	return &b
}

func Int64(i int64) *int64 {
	return &i
}
