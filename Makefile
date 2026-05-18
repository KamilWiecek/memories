CLUSTER := memories

.PHONY: build dev-up dev-down dev-images apply help

build:
	docker build -t memories-api:local -f cmd/api/Dockerfile .
	docker build -t memories-cronjob:local -f cmd/cronjob/Dockerfile .
	docker build -t memories-whisper:local -f whisper/Dockerfile .

dev-up:
	kind get clusters | grep -qx $(CLUSTER) || kind create cluster --name $(CLUSTER) --config kind-config.yaml
	helm repo add ingress-nginx https://kubernetes.github.io/ingress-nginx --force-update
	helm upgrade --install ingress-nginx ingress-nginx/ingress-nginx \
	  --namespace ingress-nginx --create-namespace \
	  --set controller.hostPort.enabled=true \
	  --set controller.service.type=NodePort \
	  --set 'controller.tolerations[0].key=node-role.kubernetes.io/control-plane' \
	  --set 'controller.tolerations[0].effect=NoSchedule' \
	  --set-string 'controller.nodeSelector.ingress-ready=true' \
	  --wait
	$(MAKE) build _load
	kubectl apply -f k8s/base.yaml
	kubectl apply -f k8s/

dev-down:
	kind delete cluster --name $(CLUSTER)

dev-images: build _load

apply:
	kubectl apply -f k8s/base.yaml
	kubectl apply -f k8s/

_load:
	kind load docker-image memories-api:local     --name $(CLUSTER)
	kind load docker-image memories-cronjob:local --name $(CLUSTER)
	kind load docker-image memories-whisper:local --name $(CLUSTER)

help:
	@echo "Targets: build  dev-up  dev-down  dev-images  apply"
