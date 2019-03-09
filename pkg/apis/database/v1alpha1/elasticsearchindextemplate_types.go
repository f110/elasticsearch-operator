package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ElasticsearchIndexTemplateStatus defines the observed state of ElasticsearchIndexTemplate
// +k8s:openapi-gen=true
type ElasticsearchIndexTemplateStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book.kubebuilder.io/beyond_basics/generating_crd.html
}

type ElasticsearchIndexTemplateSpec struct {
	Selector      metav1.LabelSelector `json:"selector"`
	IndexTemplate string               `json:"indexTemplate"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ElasticsearchIndexTemplate is the Schema for the elasticsearchindextemplates API
// +k8s:openapi-gen=true
type ElasticsearchIndexTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ElasticsearchIndexTemplateSpec   `json:"spec,omitempty"`
	Status ElasticsearchIndexTemplateStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ElasticsearchIndexTemplateList contains a list of ElasticsearchIndexTemplate
type ElasticsearchIndexTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ElasticsearchIndexTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ElasticsearchIndexTemplate{}, &ElasticsearchIndexTemplateList{})
}
