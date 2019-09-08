package controller

import (
	"github.com/eformat/microsegmentation-operator/pkg/controller/namespace"

	"github.com/redhat-cop/microsegmentation-operator/pkg/controller/service"
)

func init() {
	// AddToManagerFuncs is a list of functions to create controllers and add them to a manager.
	AddToManagerFuncs = append(AddToManagerFuncs, service.Add)
	AddToManagerFuncs = append(AddToManagerFuncs, namespace.Add)
}
