package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ElasticsearchClusterSpec defines the desired state of ElasticsearchCluster
// +k8s:openapi-gen=true
type ElasticsearchClusterSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book.kubebuilder.io/beyond_basics/generating_crd.html
	HotWarm    bool                              `json:"hot_warm"`
	MasterNode ElasticsearchClusterNodeSpec      `json:"master_node"`
	HotNode    ElasticsearchClusterNodeSpec      `json:"hot_node"`
	WarmNode   ElasticsearchClusterNodeSpec      `json:"warm_node"`
	ClientNode ElasticsearchClusterNodeSpec      `json:"client_node"`
	Forwarder  ElasticsearchClusterForwarderSpec `json:"forwarder"`
	Exporter   ElasticsearchClusterExporterSpec  `json:"exporter"`
}

type ElasticsearchClusterNodeSpec struct {
	Count        int32  `json:"count"`
	StorageClass string `json:"storage_class"`
	DiskSize     string `json:"disk_size"`
	HeapSize     string `json:"heap_size"`
	Days         int32  `json:"days"`
}

type ElasticsearchClusterForwarderSpec struct {
	Count int32 `json:"count"`
}

type ElasticsearchClusterExporterSpec struct {
	Enable      bool   `json:"enable"`
	ClusterRole string `json:"cluster_role"`
}

// ElasticsearchClusterStatus defines the observed state of ElasticsearchCluster
// +k8s:openapi-gen=true
type ElasticsearchClusterStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book.kubebuilder.io/beyond_basics/generating_crd.html
	Nodes []string `json:"nodes"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ElasticsearchCluster is the Schema for the elasticsearchclusters API
// +k8s:openapi-gen=true
type ElasticsearchCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ElasticsearchClusterSpec   `json:"spec,omitempty"`
	Status ElasticsearchClusterStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ElasticsearchClusterList contains a list of ElasticsearchCluster
type ElasticsearchClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ElasticsearchCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ElasticsearchCluster{}, &ElasticsearchClusterList{})
}
