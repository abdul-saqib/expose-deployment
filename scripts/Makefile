IMAGE_NAME=expose-controller
VERSION=latest
KIND_CLUSTER=kind

.PHONY: build load deploy all

build:
	 podman build -t $(IMAGE_NAME) ../

load:
	rm -f expose-controller.tar
	podman save -o $(IMAGE_NAME).tar localhost/$(IMAGE_NAME):$(VERSION)
	kind load image-archive $(IMAGE_NAME).tar

deploy:
	 kubectl apply -f ../manifest/rbac.yaml
	 kubectl apply -f ../manifest/controller_deployment.yaml

all: build load deploy
