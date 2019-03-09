package controller

import (
	"github.com/f110/elasticsearch-operator/pkg/controller/elasticsearchindextemplate"
)

func init() {
	// AddToManagerFuncs is a list of functions to create controllers and add them to a manager.
	AddToManagerFuncs = append(AddToManagerFuncs, elasticsearchindextemplate.Add)
}
