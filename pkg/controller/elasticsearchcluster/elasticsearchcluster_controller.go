package elasticsearchcluster

import (
	"bytes"
	"context"
	"strconv"
	"text/template"

	monitoringv1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	databasev1alpha1 "github.com/f110/elasticsearch-operator/pkg/apis/database/v1alpha1"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	batchv1beta1 "k8s.io/api/batch/v1beta1"
	corev1 "k8s.io/api/core/v1"
	policyv1beta1 "k8s.io/api/policy/v1beta1"
	rbacv1 "k8s.io/api/rbac/v1"
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

var log4j2Conf = `status = error
appender.console.type = Console
appender.console.name = console
appender.console.layout.type = PatternLayout
appender.console.layout.pattern = [%d{ISO8601}][%-5p][%-25c{1.}] %marker%m%n
rootLogger.level = info
rootLogger.appenderRef.console.ref = console
logger.searchguard.name = com.floragunn
logger.searchguard.level = info`

var dataNodeConf = `node:
	name: ${HOSTNAME}
	data: {{ .Data }}
	master: {{ .Master }}
	ingest: {{ .Ingeset }}

cluster:
	name: {{ .Name }}

network:
	host: 0.0.0.0

discovery:
	zen:
		minimum_master_nodes: {{ .MinMasterNodes }}
		ping.unicast.hosts: {{ .Name }}-master
    
gateway:
	expected_master_nodes: 2
	expected_data_nodes: 1
	recover_after_time: 5m
	recover_after_master_nodes: 2
	recover_after_data_nodes: 1

processors: ${PROCESSORS:}`

var forwarderConf = `<source>
  @type forward
  port 24224
  bind 0.0.0.0
</source>

{{ if .Exporter }}
<source>
  @type prometheus
  bind 0.0.0.0
  port 24231
</source>

<source>
  @type monitor_agent
</source>

<source>
  @type prometheus_monitor
  <labels>
    host ${hostname}
  </labels>
</source>

<source>
  @type prometheus_output_monitor
  <labels>
    host ${hostname}
  </labels>
</source>

<source>
  @type prometheus_tail_monitor
  <labels>
    host ${hostname}
  </labels>
</source>
{{ end }}

<match **>
  @type copy
  <store>
    @type elasticsearch
    hosts {{ .Name }}-client:9200
    logstash_format true
    logstash_prefix ${tag}
    <buffer>
      @type file
      path /var/log/buffer/${tag}.{{ .Name }}-client
    </buffer>
  </store>
</match>`

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
	configMap := newConfigMapLog4j2ForCR(instance)
	if res, err := r.configMapReconcile(reqLogger, instance, configMap); err != nil {
		return res, err
	}
	configMaps := newConfigMapsElasticsearchForCR(instance)
	for _, configMap := range configMaps {
		if res, err := r.configMapReconcile(reqLogger, instance, configMap); err != nil {
			return res, err
		}
	}
	masterStatefulset := newMasterStatefulsetForCR(instance)
	if res, err := r.statefulNodeReconcile(reqLogger, instance, masterStatefulset); err != nil {
		return res, err
	}
	masterService := newMasterServiceForCR(instance)
	if res, err := r.serviceReconcile(reqLogger, instance, masterService); err != nil {
		return res, err
	}
	masterPDB := newMasterPodDisruptionBudgetForCR(instance)
	if res, err := r.podDisruptionBudgetReconcile(reqLogger, instance, masterPDB); err != nil {
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
	clientService := newClientServiceForCR(instance)
	if res, err := r.serviceReconcile(reqLogger, instance, clientService); err != nil {
		return res, err
	}
	curatorConfigMap := newCuratorConfigMapForCR(instance)
	if res, err := r.configMapReconcile(reqLogger, instance, curatorConfigMap); err != nil {
		return res, err
	}
	curatorCronJob := newCuratorCronJobForCR(instance)
	if res, err := r.cronJobReconcile(reqLogger, instance, curatorCronJob); err != nil {
		return res, err
	}
	exporterServiceAccount := newExporterServiceAccountForCR(instance)
	if res, err := r.serviceAccountReconcile(reqLogger, instance, exporterServiceAccount); err != nil {
		return res, err
	}
	exporterClusterRoleBinding := newExporterClusterRoleBindingForCR(instance)
	if res, err := r.clusterRoleBindingReconcile(reqLogger, instance, exporterClusterRoleBinding); err != nil {
		return res, err
	}
	exporterDeployment := newExporterDeploymentForCR(instance)
	if res, err := r.deploymentReconcile(reqLogger, instance, exporterDeployment); err != nil {
		return res, err
	}
	exporterSerivce := newExporterServiceForCR(instance)
	if res, err := r.serviceReconcile(reqLogger, instance, exporterSerivce); err != nil {
		return res, err
	}
	exporterServiceMonitor := newExporterServiceMonitorForCR(instance)
	if res, err := r.serviceMonitorReconcile(reqLogger, instance, exporterServiceMonitor); err != nil {
		return res, err
	}
	forwarderConfigMap := newForwarderConfigMapForCR(instance)
	if res, err := r.configMapReconcile(reqLogger, instance, forwarderConfigMap); err != nil {
		return res, err
	}
	forwarderPDB := newForwarderPodDisruptionBudgetForCR(instance)
	if res, err := r.podDisruptionBudgetReconcile(reqLogger, instance, forwarderPDB); err != nil {
		return res, err
	}
	forwarderStatefulset := newForwarderStatefulSetForCR(instance)
	if res, err := r.statefulNodeReconcile(reqLogger, instance, forwarderStatefulset); err != nil {
		return res, err
	}
	forwarderService := newForwarderServiceForCR(instance)
	if res, err := r.serviceReconcile(reqLogger, instance, forwarderService); err != nil {
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

func (r *ReconcileElasticsearchCluster) serviceReconcile(reqLogger logr.Logger, instance *databasev1alpha1.ElasticsearchCluster, service *corev1.Service) (reconcile.Result, error) {
	if err := controllerutil.SetControllerReference(instance, service, r.scheme); err != nil {
		return reconcile.Result{}, err
	}

	found := &corev1.Service{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: service.Name, Namespace: service.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating a new Service for client nodes", "Service.Namespace", service.Namespace, "Service.Name", service.Name)
		err = r.client.Create(context.TODO(), service)
		if err != nil {
			return reconcile.Result{}, err
		}

		return reconcile.Result{}, nil
	} else if err != nil {
		return reconcile.Result{}, err
	}

	reqLogger.Info("Updating exists Service", "Service.Namespace", found.Namespace, "Service.Name", found.Name)
	if err := r.client.Update(context.TODO(), service); err != nil {
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}

func (r *ReconcileElasticsearchCluster) podDisruptionBudgetReconcile(reqLogger logr.Logger, instance *databasev1alpha1.ElasticsearchCluster, pdb *policyv1beta1.PodDisruptionBudget) (reconcile.Result, error) {
	if err := controllerutil.SetControllerReference(instance, pdb, r.scheme); err != nil {
		return reconcile.Result{}, err
	}

	found := &policyv1beta1.PodDisruptionBudget{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: pdb.Name, Namespace: pdb.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating a new PodDisruptionBudget", "PodDisruptionBudget.Namespace", pdb.Namespace, "PodDisruptionBudget.Name", pdb.Name)
		err = r.client.Create(context.TODO(), pdb)
		if err != nil {
			return reconcile.Result{}, err
		}

		return reconcile.Result{}, nil
	} else if err != nil {
		return reconcile.Result{}, err
	}

	reqLogger.Info("Updating exists PodDisruptionBudget", "PodDisruptionBudget.Namespace", found.Namespace, "PodDisruptionBudget.Name", found.Name)
	if err := r.client.Update(context.TODO(), pdb); err != nil {
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}

func (r *ReconcileElasticsearchCluster) configMapReconcile(reqLogger logr.Logger, instance *databasev1alpha1.ElasticsearchCluster, configMap *corev1.ConfigMap) (reconcile.Result, error) {
	if err := controllerutil.SetControllerReference(instance, configMap, r.scheme); err != nil {
		return reconcile.Result{}, err
	}

	found := &corev1.ConfigMap{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: configMap.Name, Namespace: configMap.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating a new ConfigMap", "ConfigMap.Namespace", configMap.Namespace, "ConfigMap.Name", configMap.Name)
		err = r.client.Create(context.TODO(), configMap)
		if err != nil {
			return reconcile.Result{}, err
		}

		return reconcile.Result{}, nil
	} else if err != nil {
		return reconcile.Result{}, err
	}

	reqLogger.Info("Updating exists ConfigMap", "ConfigMap.Namespace", found.Namespace, "ConfigMap.Name", found.Name)
	if err := r.client.Update(context.TODO(), configMap); err != nil {
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}

func (r *ReconcileElasticsearchCluster) cronJobReconcile(reqLogger logr.Logger, instance *databasev1alpha1.ElasticsearchCluster, cronJob *batchv1beta1.CronJob) (reconcile.Result, error) {
	if err := controllerutil.SetControllerReference(instance, cronJob, r.scheme); err != nil {
		return reconcile.Result{}, err
	}

	found := &batchv1beta1.CronJob{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: cronJob.Name, Namespace: cronJob.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating a new CronJob", "CronJob.Namespace", cronJob.Namespace, "CronJob.Name", cronJob.Name)
		err = r.client.Create(context.TODO(), cronJob)
		if err != nil {
			return reconcile.Result{}, err
		}

		return reconcile.Result{}, nil
	} else if err != nil {
		return reconcile.Result{}, err
	}

	reqLogger.Info("Updating exists CronJob", "CronJob.Namespace", found.Namespace, "CronJob.Name", found.Name)
	if err := r.client.Update(context.TODO(), cronJob); err != nil {
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}

func (r *ReconcileElasticsearchCluster) serviceAccountReconcile(reqLogger logr.Logger, instance *databasev1alpha1.ElasticsearchCluster, sa *corev1.ServiceAccount) (reconcile.Result, error) {
	if err := controllerutil.SetControllerReference(instance, sa, r.scheme); err != nil {
		return reconcile.Result{}, err
	}

	found := &corev1.ServiceAccount{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: sa.Name, Namespace: sa.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating a new ServiceAccount", "ServiceAccount.Namespace", sa.Namespace, "ServiceAccount.Name", sa.Name)
		err = r.client.Create(context.TODO(), sa)
		if err != nil {
			return reconcile.Result{}, err
		}

		return reconcile.Result{}, nil
	} else if err != nil {
		return reconcile.Result{}, err
	}

	reqLogger.Info("Updating exists ServiceAccount", "ServiceAccount.Namespace", found.Namespace, "ServiceAccount.Name", found.Name)
	if err := r.client.Update(context.TODO(), sa); err != nil {
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}

func (r *ReconcileElasticsearchCluster) clusterRoleBindingReconcile(reqLogger logr.Logger, instance *databasev1alpha1.ElasticsearchCluster, crb *rbacv1.ClusterRoleBinding) (reconcile.Result, error) {
	if err := controllerutil.SetControllerReference(instance, crb, r.scheme); err != nil {
		return reconcile.Result{}, err
	}

	found := &corev1.ServiceAccount{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: crb.Name, Namespace: crb.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating a new ClusterRoleBinding", "ClusterRoleBinding.Namespace", crb.Namespace, "ClusterRoleBinding.Name", crb.Name)
		err = r.client.Create(context.TODO(), crb)
		if err != nil {
			return reconcile.Result{}, err
		}

		return reconcile.Result{}, nil
	} else if err != nil {
		return reconcile.Result{}, err
	}

	reqLogger.Info("Updating exists ClusterRoleBinding", "ClusterRoleBinding.Namespace", found.Namespace, "ClusterRoleBinding.Name", found.Name)
	if err := r.client.Update(context.TODO(), crb); err != nil {
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}

func (r *ReconcileElasticsearchCluster) serviceMonitorReconcile(reqLogger logr.Logger, instance *databasev1alpha1.ElasticsearchCluster, sm *monitoringv1.ServiceMonitor) (reconcile.Result, error) {
	if err := controllerutil.SetControllerReference(instance, sm, r.scheme); err != nil {
		return reconcile.Result{}, err
	}

	found := &monitoringv1.ServiceMonitor{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: sm.Name, Namespace: sm.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating a new ServiceMonitor", "ServiceMonitor.Namespace", sm.Namespace, "ServiceMonitor.Name", sm.Name)
		err = r.client.Create(context.TODO(), sm)
		if err != nil {
			return reconcile.Result{}, err
		}

		return reconcile.Result{}, nil
	} else if err != nil {
		return reconcile.Result{}, err
	}

	reqLogger.Info("Updating exists ServiceMonitor", "ServiceMonitor.Namespace", found.Namespace, "ServiceMonitor.Name", found.Name)
	if err := r.client.Update(context.TODO(), sm); err != nil {
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

func newExporterDeploymentForCR(cr *databasev1alpha1.ElasticsearchCluster) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: cr.Name + "-exporter",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: Int32(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"role": "exporter",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"role": "exporter",
					},
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: cr.Name + "-exporter",
					Containers: []corev1.Container{
						{
							Name:            cr.Name,
							Image:           "justwatch/elasticsearch_exporter:1.0.4rc1",
							ImagePullPolicy: corev1.PullIfNotPresent,
							Args:            []string{"-web.listen-address", ":9109", "-es.uri", "http://{{ .Release.Name }}-client:9200", "-es.all", "-es.indices", "-es.shards"},
							ReadinessProbe: &corev1.Probe{
								Handler: corev1.Handler{
									HTTPGet: &corev1.HTTPGetAction{
										Port: intstr.FromInt(9109),
										Path: "/health",
									},
								},
							},
						},
						{
							Name:            "kube-rbac-proxy",
							Image:           "quay.io/brancz/kube-rbac-proxy:v0.4.0",
							ImagePullPolicy: corev1.PullIfNotPresent,
							Args:            []string{"--secure-listen-address=:9108", "--upstream=http://127.0.0.1:9109/"},
							Ports: []corev1.ContainerPort{
								{Name: "https", ContainerPort: 9108},
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									"memory": resource.MustParse("20Mi"),
									"cpu":    resource.MustParse("10m"),
								},
								Limits: corev1.ResourceList{
									"memory": resource.MustParse("40Mi"),
									"cpu":    resource.MustParse("20m"),
								},
							},
						},
					},
				},
			},
		},
	}
}

func newClientServiceForCR(cr *databasev1alpha1.ElasticsearchCluster) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: cr.Name + "-client",
			Annotations: map[string]string{
				"cloud.google.com/load-balancer-type": "Internal",
			},
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeLoadBalancer,
			Selector: map[string]string{
				"role": "client",
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       9200,
					Protocol:   corev1.ProtocolTCP,
					TargetPort: intstr.FromInt(9200),
				},
			},
		},
	}
}

func newMasterServiceForCR(cr *databasev1alpha1.ElasticsearchCluster) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: cr.Name + "-master",
			Annotations: map[string]string{
				"service.alpha.kubernets.io/tolerate-unready-endpoints": "true",
			},
		},
		Spec: corev1.ServiceSpec{
			Type:      corev1.ServiceTypeClusterIP,
			ClusterIP: corev1.ClusterIPNone,
			Selector: map[string]string{
				"role": "master",
			},
			PublishNotReadyAddresses: true,
			Ports: []corev1.ServicePort{
				{
					Name:       "internal",
					Port:       9300,
					Protocol:   corev1.ProtocolTCP,
					TargetPort: intstr.FromInt(9300),
				},
			},
		},
	}
}

func newMasterPodDisruptionBudgetForCR(cr *databasev1alpha1.ElasticsearchCluster) *policyv1beta1.PodDisruptionBudget {
	minAvailable := intstr.FromInt(int(cr.Spec.MasterNode.Count - 1))
	return &policyv1beta1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name: cr.Name + "-master",
		},
		Spec: policyv1beta1.PodDisruptionBudgetSpec{
			MinAvailable: &minAvailable,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"role": "master",
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

func newConfigMapLog4j2ForCR(cr *databasev1alpha1.ElasticsearchCluster) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: cr.Name + "-log4j2-conf",
		},
		Data: map[string]string{
			"log4j2.properties": log4j2Conf,
		},
	}
}

func newConfigMapsElasticsearchForCR(cr *databasev1alpha1.ElasticsearchCluster) []*corev1.ConfigMap {
	t, err := template.New("").Parse(dataNodeConf)
	if err != nil {
		return nil
	}

	configMaps := make([]*corev1.ConfigMap, 0)
	buf := &bytes.Buffer{}

	// for master node
	params := struct {
		Data           bool
		Master         bool
		Ingest         bool
		Name           string
		MinMasterNodes int32
	}{
		Data:           false,
		Master:         true,
		Ingest:         false,
		Name:           cr.Name,
		MinMasterNodes: cr.Spec.MasterNode.Count - 1,
	}
	if err := t.Execute(buf, &params); err != nil {
		return nil
	}
	configMaps = append(configMaps, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: cr.Name + "master-es-conf",
		},
		Data: map[string]string{
			"elasticsearch.yml": buf.String(),
		},
	})
	buf.Reset()

	// for hot and warm node
	params.Data = true
	params.Master = false
	if err := t.Execute(buf, &params); err != nil {
		return nil
	}
	configMaps = append(configMaps, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: cr.Name + "hot-es-conf",
		},
		Data: map[string]string{
			"elasticsearch.yml": buf.String(),
		},
	}, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: cr.Name + "warm-es-conf",
		},
		Data: map[string]string{
			"elasticsearch.yml": buf.String(),
		},
	})
	buf.Reset()

	// for client node
	params.Data = false
	params.Master = false
	params.Ingest = true
	if err := t.Execute(buf, &params); err != nil {
		return nil
	}
	configMaps = append(configMaps, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: cr.Name + "client-es-conf",
		},
		Data: map[string]string{
			"elasticsearch.yml": buf.String(),
		},
	})

	return configMaps
}

func newCuratorConfigMapForCR(cr *databasev1alpha1.ElasticsearchCluster) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: cr.Name + "-curator-conf",
		},
		Data: map[string]string{
			"config.yml": `client:
  hosts:
    - ` + cr.Name + `-client
  port: 9200`,
			"actions.yml": `actions:
  1:
    action: allocation
    description: "Apply shard allocation filtering rules to the specified indices"
    options:
      key: box_type
      value: warm
      allocation_type: require
      wait_for_completion: true
      ignore_empty_list: true
      timeout_override:
      disable_action: false
    filters:
      - filtertype: age
        source: name
        direction: older
        timestring: '%Y.%m.%d'
        unit: days
        unit_count: ` + strconv.Itoa(int(cr.Spec.HotNode.Days)) + `
  2:
    action: forcemerge
    description: "Perform a forceMerge on selected indices to 'max_num_segments' per shard"
    options:
      max_num_segments: 1
      delay:
      timeout_override: 21600
      ignore_empty_list: true
      disable_action: false
    filters:
      - filtertype: age
        source: name
        direction: older
        timestring: '%Y.%m.%d'
        unit: days
        unit_count: ` + strconv.Itoa(int(cr.Spec.HotNode.Days)) + `
  3:
    action: delete_indices
    description: Delete indices
    options:
      ignore_empty_list: true
      timeout_override:
      continue_if_exception: false
      disable_action: false
    filters:
      - filtertype: age
        source: name
        direction: older
        timestring: '%Y.%m.%d'
        unit: days
        unit_count: ` + strconv.Itoa(int(cr.Spec.WarmNode.Days)),
		},
	}
}

func newCuratorCronJobForCR(cr *databasev1alpha1.ElasticsearchCluster) *batchv1beta1.CronJob {
	return &batchv1beta1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name: cr.Name + "-curator",
		},
		Spec: batchv1beta1.CronJobSpec{
			Schedule: "0 1 * * *",
			JobTemplate: batchv1beta1.JobTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name: cr.Name + "-curator",
				},
				Spec: batchv1.JobSpec{
					BackoffLimit: Int32(1),
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"role": "curator",
							},
						},
						Spec: corev1.PodSpec{
							RestartPolicy: corev1.RestartPolicyNever,
							Containers: []corev1.Container{
								{
									Name:            "curator",
									Image:           "bobrik/curator:5.5.4",
									ImagePullPolicy: corev1.PullIfNotPresent,
									Args:            []string{"--config", "/usr/share/elasticsearch-curator/config/config.yml", "/usr/share/elasticsearch-curator/config/actions.yml"},
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      "conf",
											MountPath: "/usr/share/elasticsearch-curator/config",
										},
									},
								},
							},
							Volumes: []corev1.Volume{
								{
									Name: "conf",
									VolumeSource: corev1.VolumeSource{
										ConfigMap: &corev1.ConfigMapVolumeSource{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: cr.Name + "-curator-conf",
											},
										},
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

func newExporterServiceForCR(cr *databasev1alpha1.ElasticsearchCluster) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: cr.Name + "-exporter",
			Labels: map[string]string{
				"role": "exporter",
			},
		},
		Spec: corev1.ServiceSpec{
			Type:      corev1.ServiceTypeClusterIP,
			ClusterIP: corev1.ClusterIPNone,
			Ports: []corev1.ServicePort{
				{
					Name:       "https",
					Protocol:   corev1.ProtocolTCP,
					TargetPort: intstr.FromInt(9108),
					Port:       9108,
				},
			},
			Selector: map[string]string{
				"role": "exporter",
			},
		},
	}
}

func newExporterServiceAccountForCR(cr *databasev1alpha1.ElasticsearchCluster) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name: cr.Name + "-exporter",
		},
	}
}

func newExporterClusterRoleBindingForCR(cr *databasev1alpha1.ElasticsearchCluster) *rbacv1.ClusterRoleBinding {
	return &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: cr.Name + "-exporter",
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     cr.Spec.Exporter.ClusterRole,
		},
	}
}

func newExporterServiceMonitorForCR(cr *databasev1alpha1.ElasticsearchCluster) *monitoringv1.ServiceMonitor {
	return &monitoringv1.ServiceMonitor{
		ObjectMeta: metav1.ObjectMeta{
			Name: cr.Name + "-exporter",
			Labels: map[string]string{
				"k8s-app": cr.Name + "-exporter",
			},
		},
		Spec: monitoringv1.ServiceMonitorSpec{
			JobLabel: "k8s-app",
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"role": "exporter",
				},
			},
			NamespaceSelector: monitoringv1.NamespaceSelector{
				MatchNames: []string{cr.Namespace},
			},
			Endpoints: []monitoringv1.Endpoint{
				{
					Port:            "https",
					Scheme:          "https",
					Interval:        "60s",
					HonorLabels:     true,
					BearerTokenFile: "/var/run/secrets/kubernetes.io/serviceaccount/token",
					TLSConfig: &monitoringv1.TLSConfig{
						InsecureSkipVerify: true,
					},
				},
			},
		},
	}
}

func newForwarderConfigMapForCR(cr *databasev1alpha1.ElasticsearchCluster) *corev1.ConfigMap {
	t, err := template.New("").Parse(forwarderConf)
	if err != nil {
		return nil
	}

	buf := &bytes.Buffer{}
	params := struct {
		Name     string
		Exporter bool
	}{
		Name:     cr.Name,
		Exporter: cr.Spec.Exporter.Enable,
	}
	if err := t.Execute(buf, &params); err != nil {
		return nil
	}

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: cr.Name + "-forwarder-conf",
		},
		Data: map[string]string{
			"forwarder.conf": buf.String(),
		},
	}
}

func newForwarderPodDisruptionBudgetForCR(cr *databasev1alpha1.ElasticsearchCluster) *policyv1beta1.PodDisruptionBudget {
	minAvailable := intstr.FromInt(int(cr.Spec.Forwarder.Count - 1))
	return &policyv1beta1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name: cr.Name + "-forwarder",
		},
		Spec: policyv1beta1.PodDisruptionBudgetSpec{
			MinAvailable: &minAvailable,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"role": "forwarder",
				},
			},
		},
	}
}

func newForwarderServiceForCR(cr *databasev1alpha1.ElasticsearchCluster) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: cr.Name + "-forwarder",
			Annotations: map[string]string{
				"cloud.google.com/load-balancer-type": "Internal",
			},
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeLoadBalancer,
			Ports: []corev1.ServicePort{
				{
					Name:       "fluentd",
					Protocol:   corev1.ProtocolTCP,
					TargetPort: intstr.FromInt(24224),
					Port:       24224,
				},
			},
			Selector: map[string]string{
				"role": "forwarder",
			},
		},
	}
}

func newForwarderStatefulSetForCR(cr *databasev1alpha1.ElasticsearchCluster) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: cr.Name + "-forwarder",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &cr.Spec.Forwarder.Count,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"role": "forwarder",
				},
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: cr.Name + "-forwarder-buffer",
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						StorageClassName: &cr.Spec.Forwarder.StorageClass,
						Resources: corev1.ResourceRequirements{
							Requests: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceStorage: resource.MustParse(cr.Spec.Forwarder.DiskSize),
							},
						},
					},
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"role": "forwarder",
					},
				},
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{
							Name:            "chown",
							Image:           "fluent/fluentd:v1.4.0",
							ImagePullPolicy: corev1.PullIfNotPresent,
							Command:         []string{"/bin/bash", "-c", "set -e; set -x; chown fluent:fluent /var/log/buffer; for datadir in $(find /var/log/buffer -mindepth 1 -maxdepth 1); do chown -R fluent:fluent $datadir; done;"},
							SecurityContext: &corev1.SecurityContext{
								RunAsUser: Int64(0),
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      cr.Name + "-forwarder-buffer",
									MountPath: "/var/log/buffer",
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:            "forwarder",
							Image:           "fluent/fluentd:v1.4.0",
							ImagePullPolicy: corev1.PullIfNotPresent,
							Ports: []corev1.ContainerPort{
								{Name: "fluentd", Protocol: corev1.ProtocolTCP, ContainerPort: 24224},
							},
							LivenessProbe: &corev1.Probe{
								InitialDelaySeconds: 600,
								PeriodSeconds:       60,
								Handler: corev1.Handler{
									Exec: &corev1.ExecAction{
										Command: []string{"/bin/sh", "-c", `LIVENESS_THRESHOLD_SECONDS=${LIVENESS_THRESHOLD_SECONDS:-300};
											STUCK_THRESHOLD_SECONDS=${LIVENESS_THRESHOLD_SECONDS:-900};
											if [ ! -e /var/log/buffer ];
											then
											exit 1;
											fi;
											touch -d "${STUCK_THRESHOLD_SECONDS} seconds ago" /tmp/marker-stuck;
											if [[ -z "$(find /var/log/buffer -type f -newer /tmp/marker-stuck -print -quit)" ]];
											then
											exit 1;
											fi;
											touch -d "${LIVENESS_THRESHOLD_SECONDS} seconds ago" /tmp/marker-liveness;
											if [[ -z "$(find /var/log/buffer -type f -newer /tmp/marker-liveness -print -quit)" ]];
											then
											exit 1;
											fi;`},
									},
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "conf",
									MountPath: "/fluentd/etc/fluent.conf",
									SubPath:   "forwarder.conf",
								},
								{
									Name:      cr.Name + "-forwarder-buffer",
									MountPath: "/var/log/buffer",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "conf",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: cr.Name + "-forwarder-conf",
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

func Bool(b bool) *bool {
	return &b
}

func Int64(i int64) *int64 {
	return &i
}

func Int32(i int32) *int32 {
	return &i
}
