# Microsegmentation Operator

## Deploying the Operator

This is a cluster-level operator that you can deploy in any namespace, `microsegmentation-operator` is recommended.

```shell
oc new-project microsegmentation-operator
```

Deploy the cluster resources. Given that a number of elevated permissions are required to resources at a cluster scope the account you are currently logged in must have elevated rights.

```shell
oc apply -f deploy/
```

`OpenShift implements v1 of NetworkPolicy` : so egress rules, ipblock are not implemeneted by the default openshift-sdn.

## Configuring Operator Using Annotations

[![Build Status](https://travis-ci.org/redhat-cop/microsegmentation-operator.svg?branch=master)](https://travis-ci.org/redhat-cop/microsegmentation-operator) [![Docker Repository on Quay](https://quay.io/repository/redhat-cop/microsegmentation-operator/status "Docker Repository on Quay")](https://quay.io/repository/redhat-cop/microsegmentation-operator)

The microsegmentation operator allows to create [NetworkPolicies](https://kubernetes.io/docs/concepts/services-networking/network-policies/) rules starting from [Namespaces](https://kubernetes.io/docs/concepts/overview/working-with-objects/namespaces/) and/or [Services](https://kubernetes.io/docs/concepts/services-networking/service/).

This feature is activated by this annotation: `microsegmentation-operator.redhat-cop.io/microsegmentation: "true"`.

```
oc annotate namespace test microsegmentation-operator.redhat-cop.io/microsegmentation='true'
# AND/OR
oc annotate service test-service microsegmentation-operator.redhat-cop.io/microsegmentation='true'
```

NetworkPolicy can be controlled by annotation `Namespace` and/or `Service`. If you wish to disable or delete NetworkPolicy, set the annotation to `false`.

#### Default Deny NetworkPolicy

By default when enabled a `deny-by-default` NetworkPolicy is applied (secure by default). This is equivalent to the following policy:

```
oc apply -f - <<'EOF'
kind: NetworkPolicy
apiVersion: networking.k8s.io/v1
metadata:
  name: deny-by-default
spec:
  podSelector:
  ingress: []
EOF
```

You would then layer other policy into your namespace to allow traffic as follows in the next sections. Namespace and Port/Protocol network policy are created as separate NetworkPolicy objects.

#### Namespace control

The Namespace annotation controls access from other namespaces using annotations and namespace labels. Normal users can be restricted from editing namespaces (but will normally have self-service admin/edit access to services) within a cluster.

| Annotation  | Description  |
| - | - |
| `microsegmentation-operator.redhat-cop.io/inbound-namespace-labels`  | comma separated list of labels to be used as label selectors for allowed inbound namespaces; e.g. `key1=value1,key2=value2`  |
| `microsegmentation-operator.redhat-cop.io/outbound-namespace-labels`  | comma separated list of labels to be used as label selectors for allowed outbound namespaces; e.g. `key1=value1,key2=value2`  |
| `microsegmentation-operator.redhat-cop.io/allow-from-self`  | allow traffic from within the same namespace (true|false) |

#### Service control

Port/Protocol NetworkPolicy controls access to ports and protocols described on the service using annotations.

The NetworkPolicy object can be tweaked with the following additional annotations:

| Annotation  | Description  |
| - | - |
| `microsegmentation-operator.redhat-cop.io/additional-inbound-ports`  | comma separated list of allowed inbound ports expressed in this format: *port/protocol*; e.g. `8888/TCP,9999/UDP`  |
|  `microsegmentation-operator.redhat-cop.io/inbound-pod-labels` | comma separated list of labels to be used as label selectors for allowed inbound pods; e.g. `key1=value1,key2=value2`  |
| `microsegmentation-operator.redhat-cop.io/outbound-pod-labels`  | comma separated list of labels to be used as label selectors for allowed outbound pods; e.g. `key1=value1,key2=value2`  ||   |   |
| `microsegmentation-operator.redhat-cop.io/outbound-ports`  | comma separated list of allowed outbound ports expressed in this format: *port/protocol*; e.g. `8888/TCP,9999/UDP`  |

Inbound/outbound ports are `AND` 'ed with corresponding inbound/outbound pod label selectors.

It should be relatively common to use the `additional-inbound-ports` annotation to model those situation where a pod exposes a port that should not be load balanced.

## Examples

See test directory for an example.

```
oc apply -f test/simple-microsegmentation.yaml
```

## Local Development

Execute the following steps to develop the functionality locally. It is recommended that development be done using a cluster with `cluster-admin` permissions.

Clone the repository, then resolve all dependencies using `dep`:

```shell
dep ensure
```

Using the [operator-sdk](https://github.com/operator-framework/operator-sdk), run the operator locally:

```shell
operator-sdk up local --namespace "test" --verbose
```

Use delve debugger

```
operator-sdk up local --namespace "test" --verbose --enable-delve
```

With a remote debug `launch.json` in vscode:

```
    {
      "name": "Launch remote",
      "type": "go",
      "request": "launch",
      "mode": "remote",
      "port": 2345,
      "host": "127.0.0.1",
      "remotePath": "",
      "program": "${workspaceFolder}/build/_output/bin/microsegmentation-operator-local",
      "trace": "log",
      "env": {
        "GOPATH": "/usr/bin/go",
        "WATCH_NAMESPACE": "test"
      }
    }
```