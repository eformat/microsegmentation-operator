package namespace

import (
	"context"
	"fmt"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	networkv1 "k8s.io/api/networking/v1"

	"github.com/redhat-cop/operator-utils/pkg/util"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("controller_namespace")

const annotationBase = "microsegmentation-operator.redhat-cop.io"
const microsgmentationAnnotation = annotationBase + "/microsegmentation"
const inboundNamespaceLabels = annotationBase + "/inbound-namespace-labels"
const outboundNamespaceLabels = annotationBase + "/outbound-namespace-labels"
const allowFromSelfLabel = annotationBase + "/allow-from-self"
const controllerName = "namespace-controller"

// Add creates a new Namespace Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileNamespace{
		ReconcilerBase: util.NewReconcilerBase(mgr.GetClient(), mgr.GetScheme(), mgr.GetConfig(), mgr.GetRecorder(controllerName)),
	}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New(controllerName, mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	isAnnotatedNamespace := predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			_, ok := e.ObjectOld.(*corev1.Namespace)
			if !ok {
				return false
			}
			_, ok = e.ObjectNew.(*corev1.Namespace)
			if !ok {
				return false
			}
			oldValueMS, _ := e.MetaOld.GetAnnotations()[microsgmentationAnnotation]
			newValueMS, _ := e.MetaNew.GetAnnotations()[microsgmentationAnnotation]
			oldMS := oldValueMS == "true"
			newMS := newValueMS == "true"
			oldValueAS, _ := e.MetaOld.GetAnnotations()[allowFromSelfLabel]
			newValueAS, _ := e.MetaNew.GetAnnotations()[allowFromSelfLabel]
			oldAS := oldValueAS == "true"
			newAS := newValueAS == "true"
			return (oldMS != newMS) || (oldAS != newAS)
		},
		CreateFunc: func(e event.CreateEvent) bool {
			_, ok := e.Object.(*corev1.Namespace)
			if !ok {
				return false
			}
			value, _ := e.Meta.GetAnnotations()[microsgmentationAnnotation]
			return value == "true"
		},
	}

	// Watch for changes to primary resource Namespace
	err = c.Watch(&source.Kind{Type: &corev1.Namespace{}}, &handler.EnqueueRequestForObject{}, isAnnotatedNamespace)
	if err != nil {
		return err
	}

	// Watch for changes to secondary resource and requeue the owner Namespace
	err = c.Watch(&source.Kind{Type: &networkv1.NetworkPolicy{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &corev1.Namespace{},
	})
	if err != nil {
		return err
	}

	return nil
}

var _ reconcile.Reconciler = &ReconcileNamespace{}

// ReconcileNamespace reconciles a Namespace object
type ReconcileNamespace struct {
	util.ReconcilerBase
}

// Reconcile reads that state of the cluster for a Namespace object and makes changes based on the state read
// and what is in the Namespace.Spec
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileNamespace) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name, "Request.NamespacedName", request.NamespacedName)
	reqLogger.Info("Reconciling Namespace")

	// Fetch the Namespace instance
	instance := &corev1.Namespace{}
	// Funky NamespacedName stuff here, this should work?
	// err := r.GetClient().Get(context.TODO(), request.NamespacedName, instance)
	err := r.GetClient().Get(context.TODO(), types.NamespacedName{Name: request.NamespacedName.Name}, instance)
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

	// The object is being deleted
	if !instance.ObjectMeta.DeletionTimestamp.IsZero() {
		return reconcile.Result{}, nil
	}

	// Define a default deny all networkpolicy
	if instance.Annotations[microsgmentationAnnotation] == "true" {
		defaultNetworkPolicy := getDenyDefaultNetworkPolicy(instance)
		err = r.CreateOrUpdateResource(instance, instance.GetNamespace(), defaultNetworkPolicy)
		if err != nil {
			log.Error(err, "unable to create DefaultDenyNetworkPolicy", "NetworkPolicy", defaultNetworkPolicy)
			return r.manageError(err, instance)
		}
	}

	// Allow from self
	allowFromSelfNetworkPolicy := getAllowFromSelfNetworkPolicy(instance)

	if instance.Annotations[allowFromSelfLabel] == "true" && instance.Annotations[microsgmentationAnnotation] == "true" {
		err = r.CreateOrUpdateResource(instance, instance.GetNamespace(), allowFromSelfNetworkPolicy)
		if err != nil {
			log.Error(err, "unable to create AllowFromSelfNetworkPolicy", "NetworkPolicy", allowFromSelfNetworkPolicy)
			return r.manageError(err, instance)
		}
	} else {
		err = r.GetClient().Delete(context.TODO(), allowFromSelfNetworkPolicy)
		if err != nil {
			if errors.IsNotFound(err) {
				return reconcile.Result{}, nil
			}
			log.Error(err, "unable to delete AllowFromSelfNetworkPolicy", "NetworkPolicy", allowFromSelfNetworkPolicy)
			return r.manageError(err, instance)
		}
	}

	// Other Namespace NetworkPolicy
	networkPolicy := getNetworkPolicy(instance)

	if instance.Annotations[microsgmentationAnnotation] == "true" {
		err = r.CreateOrUpdateResource(instance, instance.GetNamespace(), networkPolicy)
		if err != nil {
			log.Error(err, "unable to create NetworkPolicy", "NetworkPolicy", networkPolicy)
			return r.manageError(err, instance)
		}
	} else {
		err = r.GetClient().Delete(context.TODO(), networkPolicy)
		if err != nil {
			if errors.IsNotFound(err) {
				return reconcile.Result{}, nil
			}
			log.Error(err, "unable to delete NetworkPolicy", "NetworkPolicy", networkPolicy)
			return r.manageError(err, instance)
		}
	}

	return reconcile.Result{}, nil
}

func getDenyDefaultNetworkPolicy(namespace *corev1.Namespace) *networkv1.NetworkPolicy {
	defaultNetworkPolicy := &networkv1.NetworkPolicy{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "networking.k8s.io/v1",
			Kind:       "NetworkPolicy",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "deny-by-default",
			Namespace: namespace.GetName(),
		},
		Spec: networkv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{},
			Egress:      []networkv1.NetworkPolicyEgressRule{},
			Ingress:     []networkv1.NetworkPolicyIngressRule{},
		},
	}
	//defaultNetworkPolicy.Spec.Ingress = append(defaultNetworkPolicy.Spec.Ingress, networkv1.NetworkPolicyIngressRule{})

	return defaultNetworkPolicy
}

func getAllowFromSelfNetworkPolicy(namespace *corev1.Namespace) *networkv1.NetworkPolicy {
	allowFromSelfNetworkPolicy := &networkv1.NetworkPolicy{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "networking.k8s.io/v1",
			Kind:       "NetworkPolicy",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "allow-from-self",
			Namespace: namespace.GetName(),
		},
		Spec: networkv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{},
			Egress:      []networkv1.NetworkPolicyEgressRule{},
			Ingress:     []networkv1.NetworkPolicyIngressRule{},
		},
	}

	networkPolicyIngressRule := networkv1.NetworkPolicyIngressRule{
		From: []networkv1.NetworkPolicyPeer{networkv1.NetworkPolicyPeer{
			NamespaceSelector: getLabelSelectorFromAnnotation("name=" + namespace.GetName()),
		}},
	}
	allowFromSelfNetworkPolicy.Spec.Ingress = append(allowFromSelfNetworkPolicy.Spec.Ingress, networkPolicyIngressRule)

	return allowFromSelfNetworkPolicy
}

func getNetworkPolicy(namespace *corev1.Namespace) *networkv1.NetworkPolicy {
	networkPolicy := &networkv1.NetworkPolicy{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "networking.k8s.io/v1",
			Kind:       "NetworkPolicy",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      namespace.GetName(),
			Namespace: namespace.GetName(),
		},
		Spec: networkv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{},
			Egress:      []networkv1.NetworkPolicyEgressRule{},
			Ingress:     []networkv1.NetworkPolicyIngressRule{},
		},
	}

	if inboundNamespaceLabels, ok := namespace.Annotations[inboundNamespaceLabels]; ok {
		networkPolicy.ObjectMeta.Name = "ingress-from-namespaces"
		networkPolicy.Spec.Ingress = getIngressRulesFromLabels(inboundNamespaceLabels)
	}
	if outboundNamespaceLabels, ok := namespace.Annotations[outboundNamespaceLabels]; ok {
		networkPolicy.ObjectMeta.Name = "egress-to-namespaces"
		networkPolicy.Spec.Egress = getEgressRulesFromLabels(outboundNamespaceLabels)
	}

	return networkPolicy
}

/*
   - from:
     - namespaceSelector:
         matchLabels:
           key1: value1
   - from:
     - namespaceSelector:
         matchLabels:
           key2: value2
*/
func getIngressRulesFromLabels(labels string) []networkv1.NetworkPolicyIngressRule {
	labelsStrings := strings.Split(labels, ",")
	rules := make([]networkv1.NetworkPolicyIngressRule, 0)
	rules = append(rules, networkv1.NetworkPolicyIngressRule{
		From: []networkv1.NetworkPolicyPeer{networkv1.NetworkPolicyPeer{
			PodSelector: &metav1.LabelSelector{},
		}},
	})
	for _, labelString := range labelsStrings {
		labelMap := map[string]string{}
		if strings.Index(labelString, "=") < 1 {
			log.Error(fmt.Errorf("Labels: %s ", labels), "FATAL: check namespace annotations - missing = sign ?", labels)
			return make([]networkv1.NetworkPolicyIngressRule, 0)
		}
		label := labelString[:strings.Index(labelString, "=")]
		value := labelString[strings.Index(labelString, "=")+1:]
		labelMap[label] = value
		lm := &metav1.LabelSelector{
			MatchLabels: labelMap,
		}
		rules = append(rules, networkv1.NetworkPolicyIngressRule{
			From: []networkv1.NetworkPolicyPeer{networkv1.NetworkPolicyPeer{
				NamespaceSelector: lm,
			}},
		})
	}
	return rules
}

func getEgressRulesFromLabels(labels string) []networkv1.NetworkPolicyEgressRule {
	labelsStrings := strings.Split(labels, ",")
	rules := make([]networkv1.NetworkPolicyEgressRule, 0)
	for _, labelString := range labelsStrings {
		labelMap := map[string]string{}
		if strings.Index(labelString, "=") < 1 {
			log.Error(fmt.Errorf("Labels: %s ", labels), "FATAL: check namespace annotations - missing = sign ?", labels)
			return make([]networkv1.NetworkPolicyEgressRule, 0)
		}
		label := labelString[:strings.Index(labelString, "=")]
		value := labelString[strings.Index(labelString, "=")+1:]
		labelMap[label] = value
		lm := &metav1.LabelSelector{
			MatchLabels: labelMap,
		}
		rules = append(rules, networkv1.NetworkPolicyEgressRule{
			To: []networkv1.NetworkPolicyPeer{networkv1.NetworkPolicyPeer{
				NamespaceSelector: lm,
			}},
		})
	}
	return rules
}

/*
   - from:
     - namespaceSelector:
         matchLabels:
					 key1: value1
					 key2: value2
*/
func getLabelSelectorFromAnnotation(labels string) *metav1.LabelSelector {
	// this annotation looks like this: label1=value,label2=value2
	labelMap := map[string]string{}
	labelsStrings := strings.Split(labels, ",")
	for _, labelString := range labelsStrings {
		if strings.Index(labelString, "=") < 1 {
			log.Error(fmt.Errorf("Labels: %s ", labels), "FATAL: check namespace annotations - missing = sign ?", labels)
			return &metav1.LabelSelector{
				MatchLabels: labelMap,
			}
		}
		label := labelString[:strings.Index(labelString, "=")]
		value := labelString[strings.Index(labelString, "=")+1:]
		labelMap[label] = value
	}
	return &metav1.LabelSelector{
		MatchLabels: labelMap,
	}
}

func (r *ReconcileNamespace) manageError(issue error, instance runtime.Object) (reconcile.Result, error) {
	r.GetRecorder().Event(instance, "Warning", "ProcessingError", issue.Error())
	return reconcile.Result{
		RequeueAfter: time.Minute * 2,
		Requeue:      true,
	}, nil
}
