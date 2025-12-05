#!/bin/bash
# =============================================================================
# KIND Setup Script for xDS Controller
# =============================================================================
# Creates a local Kubernetes cluster with KIND and deploys the xDS Controller
# with a simple nginx example demonstrating LDS, RDS, and CDS.
#
# Prerequisites:
#   - Docker
#   - kind (https://kind.sigs.k8s.io/)
#   - kubectl
#
# Usage:
#   ./kind/setup.sh
#
# Access:
#   http://localhost:8080  - Envoy HTTP proxy
#   http://localhost:9901  - Envoy admin
# =============================================================================

set -e

CLUSTER_NAME="xds-demo"
NAMESPACE="xds-system"

echo "🚀 Setting up xDS Controller demo with KIND..."

# Check prerequisites
command -v docker >/dev/null 2>&1 || { echo "❌ Docker is required but not installed."; exit 1; }
command -v kind >/dev/null 2>&1 || { echo "❌ kind is required. Install: https://kind.sigs.k8s.io/"; exit 1; }
command -v kubectl >/dev/null 2>&1 || { echo "❌ kubectl is required but not installed."; exit 1; }

# Create KIND cluster
echo "📦 Creating KIND cluster '$CLUSTER_NAME'..."
if kind get clusters 2>/dev/null | grep -q "^${CLUSTER_NAME}$"; then
    echo "   Cluster already exists, reusing..."
else
    kind create cluster --name "$CLUSTER_NAME" --config kind/cluster.yaml
fi

# Wait for cluster to be ready
echo "⏳ Waiting for cluster to be ready..."
kubectl wait --for=condition=Ready nodes --all --timeout=60s

# Apply CRDs first
echo "📋 Applying CRDs..."
kubectl apply -f config/crd/bases/

# Wait for CRDs to be established
echo "⏳ Waiting for CRDs to be established..."
kubectl wait --for=condition=Established crd/listeners.envoyxds.io --timeout=30s
kubectl wait --for=condition=Established crd/routes.envoyxds.io --timeout=30s
kubectl wait --for=condition=Established crd/clusters.envoyxds.io --timeout=30s

# Deploy xDS Controller (includes namespace, RBAC, deployment, service)
echo "🎮 Deploying xDS Controller..."
kubectl apply -f config/controller/

# Wait for controller to be ready
echo "⏳ Waiting for xDS Controller to be ready..."
kubectl wait --for=condition=Available deployment/xds-controller -n "$NAMESPACE" --timeout=120s

# Deploy Envoy (will wait for xDS config via initial_fetch_timeout: 0s)
echo "🔷 Deploying Envoy..."
kubectl apply -f config/envoy/envoy-deployment.yaml

# Patch Envoy service to use NodePort for direct access
echo "🔧 Configuring NodePort for direct access..."
kubectl apply -f kind/envoy-nodeport.yaml

# Apply demo resources (Envoy will receive these via xDS)
echo "📝 Applying demo resources (Listener, Route, Cluster, nginx)..."
kubectl apply -f config/demo/demo.yaml

# Wait for nginx to be ready
echo "⏳ Waiting for nginx backend to be ready..."
kubectl wait --for=condition=Available deployment/nginx -n "$NAMESPACE" --timeout=60s

# Wait for Envoy to receive xDS config and become ready
# (startupProbe waits up to 150s for /ready, which requires xDS config)
echo "⏳ Waiting for Envoy to receive xDS config..."
kubectl wait --for=condition=Available deployment/envoy -n "$NAMESPACE" --timeout=180s

echo ""
echo "✅ Setup complete!"
echo ""
echo "🧪 Test:"
echo "   curl http://localhost:8080"
echo ""
echo "📊 Envoy admin:"
echo "   open http://localhost:9901"
echo ""
echo "📋 View xDS resources:"
echo "   kubectl get listeners,routes,clusters -n $NAMESPACE"
echo ""
echo "🔍 Debug (if needed):"
echo "   kubectl logs deployment/xds-controller -n $NAMESPACE"
echo "   kubectl logs deployment/envoy -n $NAMESPACE"
echo ""
echo "🗑️  Cleanup:"
echo "   kind delete cluster --name $CLUSTER_NAME"
