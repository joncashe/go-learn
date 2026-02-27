# go-learn

A Kubernetes operator built from scratch with [controller-runtime](https://github.com/kubernetes-sigs/controller-runtime) that manages feature flags backed by ConfigMap keys.

## Overview

The operator introduces a `FeatureFlag` custom resource. Each `FeatureFlag` points to a key in a ConfigMap. Whenever the ConfigMap changes, the operator reconciles all `FeatureFlag` objects that reference it, updates their status, and optionally patches a Deployment's container args to reflect the new value.

```
ConfigMap change
      │
      ▼
EnqueueRequestsFromMapFunc
      │   (finds all FeatureFlags referencing this ConfigMap)
      ▼
Reconcile(FeatureFlag)
      │
      ├─► Update FeatureFlag.status (value, enabled, conditions)
      │
      └─► Patch Deployment container args  ← optional
```

## Custom Resource

```yaml
apiVersion: featureflags.example.com/v1alpha1
kind: FeatureFlag
metadata:
  name: my-feature
  namespace: default
spec:
  # Required: which ConfigMap key to read
  configMapRef:
    name: my-config
    namespace: default
    key: feature-enabled

  # Optional: value to use when ConfigMap/key is missing
  defaultValue: "false"

  # Optional: patch a Deployment container arg when the flag changes
  deploymentRef:
    name: my-app
    namespace: default
    container: app          # container name in the pod spec
    argName: "--feature"    # reconciler sets/replaces "--feature=<value>"
```

### Status fields

| Field | Type | Description |
|-------|------|-------------|
| `value` | string | Raw value read from the ConfigMap key (or `defaultValue`) |
| `enabled` | bool | `true` when value is `true`, `1`, `yes`, or `on` (case-insensitive) |
| `lastUpdated` | string | RFC3339 timestamp of last reconciliation |
| `conditions` | array | Standard Kubernetes conditions; `Ready` condition reflects ConfigMap reachability |

## Project Layout

```
.
├── main.go                              # Manager entry point
├── go.mod / go.sum                      # Module dependencies
├── api/v1alpha1/
│   ├── featureflag_types.go             # CRD Go types + DeepCopy methods
│   └── register.go                      # Scheme registration
├── controllers/
│   └── featureflag_controller.go        # Reconciler + ConfigMap secondary watch
└── config/crd/
    └── featureflag.yaml                 # CRD manifest (apply before running)
```

## Dependencies

| Package | Version |
|---------|---------|
| `sigs.k8s.io/controller-runtime` | v0.19.4 |
| `k8s.io/api` / `apimachinery` / `client-go` | v0.31.0 |
| `go.uber.org/zap` | v1.26.0 |
| Go | 1.22+ |

## Running Locally

### Prerequisites

A running cluster pointed to by your kubeconfig (e.g. [kind](https://kind.sigs.k8s.io/) or [minikube](https://minikube.sigs.k8s.io/)).

```bash
kind create cluster --name featureflags-dev
```

### 1. Apply the CRD

```bash
kubectl apply -f config/crd/featureflag.yaml
kubectl get crd featureflags.featureflags.example.com
```

### 2. Build and run

```bash
go build -o bin/manager .
./bin/manager --leader-elect=false
```

### 3. Create test resources

```bash
# ConfigMap with the flag value
kubectl create configmap my-config \
  --from-literal=feature-enabled=true \
  -n default

# FeatureFlag CR
kubectl apply -f - <<EOF
apiVersion: featureflags.example.com/v1alpha1
kind: FeatureFlag
metadata:
  name: my-feature
  namespace: default
spec:
  configMapRef:
    name: my-config
    namespace: default
    key: feature-enabled
  defaultValue: "false"
EOF
```

### 4. Verify status

```bash
kubectl get featureflags          # uses short column output
kubectl get featureflag my-feature -o yaml
```

### 5. Flip the flag

```bash
kubectl patch configmap my-config \
  --type=merge \
  -p '{"data":{"feature-enabled":"false"}}'

# The operator reconciles automatically; check updated status:
kubectl get featureflag my-feature -o jsonpath='{.status}'
```

### 6. Test Deployment arg patching

```bash
# Create a dummy Deployment
kubectl create deployment my-app --image=nginx

# Add deploymentRef to the FeatureFlag
kubectl patch featureflag my-feature --type=merge -p '{
  "spec": {
    "deploymentRef": {
      "name": "my-app",
      "namespace": "default",
      "container": "nginx",
      "argName": "--feature"
    }
  }
}'

# Verify the arg was injected
kubectl get deployment my-app \
  -o jsonpath='{.spec.template.spec.containers[0].args}'
```

## RBAC (production deployment)

The operator's ServiceAccount needs these permissions:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: featureflags-operator
rules:
  - apiGroups: ["featureflags.example.com"]
    resources: ["featureflags"]
    verbs: ["get", "list", "watch", "update", "patch"]
  - apiGroups: ["featureflags.example.com"]
    resources: ["featureflags/status"]
    verbs: ["update", "patch"]
  - apiGroups: [""]
    resources: ["configmaps"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["apps"]
    resources: ["deployments"]
    verbs: ["get", "list", "watch", "update", "patch"]
  - apiGroups: ["coordination.k8s.io"]
    resources: ["leases"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: [""]
    resources: ["events"]
    verbs: ["create", "patch"]
```
