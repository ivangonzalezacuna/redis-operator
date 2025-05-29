# redis-operator

The redis-operator is a custom Kubernetes Operator which allows creating a simple
Redis deployment using a random and secure password.

## Description

When a new `RedisOperator` is created, the user must only specify the name, namespace,
replicas and the port where the Redis instance is going to be running, based on the
[bitnami/redis](https://hub.docker.com/r/bitnami/redis) container.

For example, a YAML like [this](./config/samples/ivangonzalezacuna_v1alpha1_redisoperator.yaml):

```yaml
apiVersion: ivangonzalezacuna.docker.io/v1alpha1
kind: RedisOperator
metadata:
  labels:
    app.kubernetes.io/name: redisoperator-example
  name: redisoperator-example
  namespace: default
spec:
  replicas: 2
  port: 6379
```

## Getting Started

### Prerequisites

- go version v1.23.0+
- docker version 17.03+.
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.

#### Golang

Install following the [official documentation](https://go.dev/doc/install).

#### Docker

Install docker in your machine following the [official documentation](https://www.docker.com/get-started/).

For this setup, I've used my [personal Docker Hub repository](https://hub.docker.com/repositories/ivangonzalezacuna).

#### Kubectl

Install following the [official documentation](https://kubernetes.io/docs/tasks/tools/#kubectl).

#### Kubernetes Cluster

For this setup we can use `minikube` as our Kubernetes Cluster.
You can install it and start a cluster following the [official documentation](https://kubernetes.io/docs/tasks/tools/#minikube).

### Deploy the Custom Kubernetes Operator

In order to deploy the CRD (Custom Resource Definition) and the Manager, which will
handle the creation of the custom resources, you will need to follow these steps:

```sh
# To install the CRDs into the cluster
make install

# To deploy the Manager to the cluster
make deploy
```

Once that is done and the manager is started, you can check the logs with this command:

```sh
kubectl -n redis-operator-system logs -f redis-operator-controller-manager-85978b9cc-vkjp4
```

### Deploy the Custom Resource

Then it's time to apply a `RedisOperator` into our cluster. For that, you can directly apply the sample:

```sh
kubectl apply -f config/samples/ivangonzalezacuna_v1alpha1_redisoperator.yaml
```

After a few seconds, you should the new Deployments, Pods and Secrets:

```sh
$ kubectl -n default get deployments.apps
NAME                    READY   UP-TO-DATE   AVAILABLE   AGE
redisoperator-example   2/2     2            2           20m

$ kubectl -n default get pods
NAME                                     READY   STATUS    RESTARTS   AGE
redisoperator-example-74c6d87b9b-2w4l7   1/1     Running   0          20s
redisoperator-example-74c6d87b9b-kgg25   1/1     Running   0          20s

$ kubectl -n default get secrets
NAME           TYPE     DATA   AGE
redis-secret   Opaque   1      20m
```

Now, we can check if the password is actually set and working properly. For that, let's
see the following commands and their output:

```sh
# Make a PING without a password set
$ kubectl exec redisoperator-example-74c6d87b9b-kgg25 -- sh -c 'redis-cli -p ${REDIS_PORT_NUMBER} ping'
NOAUTH Authentication required.

# Now let's make the same request setting the password
$ kubectl exec redisoperator-example-74c6d87b9b-kgg25 -- sh -c 'REDISCLI_AUTH=${REDIS_PASSWORD} redis-cli -p ${REDIS_PORT_NUMBER} ping'
PONG

# To verify that the password is there and it's secure, we can use:
$ kubectl exec redisoperator-example-74c6d87b9b-kgg25 -- sh -c 'echo $REDIS_PASSWORD'
...
```
> **NOTE:** Make sure you set the port as well. If you specified a different `spec.port` that the 
> default one, the ping will always fail with a `Could not connect to Redis` error.

### Update the Custom Resource

This Kubernetes Operator also supports to update the resource if a `patch` is received, for example. Let's see what happens if we do so:

```sh
# Update the replicas from 2 to 5
$ kubectl patch redisoperators.ivangonzalezacuna.docker.io redisoperator-example -p '{"spec":{"replicas": 5}}' --type=merge
redisoperator.ivangonzalezacuna.docker.io/redisoperator-example patched

$ kubectl -n default get pods
NAME                                     READY   STATUS    RESTARTS   AGE
redisoperator-example-74c6d87b9b-2w4l7   1/1     Running   0          75s
redisoperator-example-74c6d87b9b-hbsvq   1/1     Running   0          5s
redisoperator-example-74c6d87b9b-kgg25   1/1     Running   0          75s
redisoperator-example-74c6d87b9b-r996r   1/1     Running   0          5s
redisoperator-example-74c6d87b9b-vpxkk   1/1     Running   0          5s
```

Also, the `replicas` is configured to always be between 1 and 5. In other case, an
error would be returned:

```sh
$ kubectl patch redisoperators.ivangonzalezacuna.docker.io redisoperator-example -p '{"spec":{"replicas": 10}}' --type=merge
The RedisOperator "redisoperator-example" is invalid: spec.replicas: Invalid value: 10: spec.replicas in body should be less than or equal to 5
```

As well, it's possible to update the port that Redis is listening to. For example:

```sh
$ kubectl patch redisoperators.ivangonzalezacuna.docker.io redisoperator-example -p '{"spec":{"port": 6789}}' --type=merge
redisoperator.ivangonzalezacuna.docker.io/redisoperator-example patched

$ kubectl get pods redisoperator-example-8597498f9c-f59hz -o yaml | grep ports: -A 5
    ports:
    - containerPort: 6789
      name: redis
      protocol: TCP
    resources: {}
    terminationMessagePath: /dev/termination-log
```

## Development

During development, it's possible to build and push new images for the Manager if needed.
Afterwards, it's important to update the Kubernetes Operator accordingly with the new
image. For that, we can use the following command:

```sh
# Build and push a new image using a new tag
make docker-build docker-push VERSION=<version>
```

If the CRDs changed in some way (new `Spec` or `Status` variables for example), then
we will also need to run the 2 following commands to ensure everything is updated:

```sh
# To update the base CRD definition
make generate

# To update any other changed manifests, like RBAC permissions
make manifests
```

If the CRDs changed, then we must ensure they are up-to-date in the cluster. For that,
execute the following command:

```sh
make install
```

And finally, to deploy the Manager version into the cluster, we will need to execute the
following command:

```sh
make deploy VERSION=<version>
```

> **IMPORTANT:** If the version pushed to the registry is different, we must ensure the
> variable `VERSION` matches it. Otherwise, the string `0.0.1` will be used by default.

### Uninstall resources

It might also be needed at some point to delete the resources created in the cluster.
For that, there are the following commands available:

```sh
# Delete the instances (CRs) from the cluster
kubectl delete -k config/samples/

# Delete the APIs(CRDs) from the cluster
make uninstall

# Undeploy the controller from the cluster
make undeploy
```
