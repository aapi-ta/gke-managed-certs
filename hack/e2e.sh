#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_ROOT=$(dirname ${BASH_SOURCE})/..
echo -ne "cd to hack\n"
cd ${SCRIPT_ROOT}/hack
echo -ne "pwd: `pwd`\n"

echo -ne "Configure registry authentication\n"
gcloud auth activate-service-account --key-file=/etc/service-account/service-account.json
gcloud auth configure-docker

echo -ne "get kubectl 1.11\n"
curl -LO https://storage.googleapis.com/kubernetes-release/release/v1.11.0/bin/linux/amd64/kubectl
chmod +x kubectl
echo -ne "kubectl version: `./kubectl version`\n"

echo -ne "set namespace default\n"
kubectl config set-context $(kubectl config current-context) --namespace=default

echo -ne "Delete components created for e2e tests\n"
./e2e-down.sh

echo -ne "Deploy components for e2e tests\n"
./e2e-up.sh

echo -ne `(kubectl get pods -o yaml)`

echo -ne "Delete components created for e2e tests\n"
./e2e-down.sh