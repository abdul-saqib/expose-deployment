#!/bin/bash
set -e


IMAGE="expose-controller:latest"
CLUSTER="kind"


echo "Building image..."
podman build -t $IMAGE .


echo "Loading image into KIND..."
kind load docker-image $IMAGE --name $CLUSTER


echo "Applying RBAC and controller deployment..."
kubectl apply -f rbac.yaml
kubectl apply -f controller-deployment.yaml


echo "Done. Controller deployed successfully."