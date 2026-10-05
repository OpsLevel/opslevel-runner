# Driving a cluster the runner does not run in

The runner resolves its Kubernetes client with
`clientcmd.NewNonInteractiveDeferredLoadingClientConfig` — a kubeconfig if
`$KUBECONFIG` or `~/.kube/config` is present, otherwise in-cluster config. So
targeting a remote cluster has always been possible in principle; the image just
had no way to authenticate to EKS.

`aws-iam-authenticator` is now in the image for exactly that. The runner itself
stays cloud-agnostic — it speaks kubeconfig, and EKS support is a property of
the image rather than of the binary. That was the reason for a standalone
authenticator over either the AWS CLI (no musl build) or the AWS SDK in Go
(which would put AWS specifics in a tool that otherwise has none).

## Why you would want this

The control plane and the workloads it schedules are different trust levels. The
runner holds an OpsLevel API token and has pod-create rights; job pods run
tenant-supplied work. Putting them in separate clusters is a stronger boundary
than separate namespaces, and it is what the agent sandbox platform's
architecture assumes — the controller schedules work but never executes tenant
content.

It is also a prerequisite for dispatching across cells later: a controller that
can only create pods in its own cluster cannot address more than one.

## Configuring it

Mount a kubeconfig and point `KUBECONFIG` at it. **The kubeconfig holds no
credentials**, so a ConfigMap is fine — SOPS is not needed:

```yaml
apiVersion: v1
kind: Config
current-context: target
clusters:
  - name: target
    cluster:
      server: https://XXXXXXXX.gr7.us-east-1.eks.amazonaws.com
      certificate-authority-data: <base64 CA>
contexts:
  - name: target
    context: { cluster: target, user: target }
users:
  - name: target
    user:
      exec:
        apiVersion: client.authentication.k8s.io/v1beta1
        command: aws-iam-authenticator
        args: ["token", "-i", "<cluster-name>"]
```

AWS credentials come from the pod's environment. On EKS that means Pod Identity
or IRSA on the ServiceAccount the runner runs as, in the cluster it runs *in*.
`aws-iam-authenticator` needs no IAM permissions of its own — `token` presigns
an STS `GetCallerIdentity` request locally and makes no AWS API call. It only
needs an identity to sign as.

## The two IAM pieces

**In the cluster the runner runs in:** a Pod Identity association binding the
runner's ServiceAccount to an IAM role. The role needs no policies attached.

**In the cluster the runner targets:** an EKS access entry for that role.

Scope the access entry to a namespace, not the cluster. An entry with
`kubernetes_groups` bound to a Role in the target namespace is enough for the
runner's work — pods, pods/exec, pods/log, configmaps — and is a much better
fit than `AmazonEKSClusterAdminPolicy`. Least privilege matters more when the
credential lives in a different cluster than the one it opens.

## Gotchas

**An unmapped identity gets 401, not 403.** With
`authentication_mode = "API"`, a principal with no access entry fails
authentication rather than authorization, so it reads as a credentials problem
and sends you looking at the wrong layer.

**Verify Pod Identity reaches the authenticator.** Pod Identity supplies
credentials through `AWS_CONTAINER_CREDENTIALS_FULL_URI` and
`AWS_CONTAINER_AUTHORIZATION_TOKEN_FILE`. The authenticator's SDK supports that,
but it is worth confirming in your deployment rather than assuming — the failure
is an opaque token error at startup.

**`AWS_REGION` must be set** if the target cluster is not in the runner's
default region.

## Updating the authenticator

Pinned by version and SHA-256 in the `Dockerfile`, with per-architecture
checksums from the release's `authenticator_<version>_checksums.txt`. Both must
be updated together; a mismatch fails the build rather than silently installing
something unexpected.
