FROM golang:alpine@sha256:3ad57304ad93bbec8548a0437ad9e06a455660655d9af011d58b993f6f615648 AS build
RUN apk --no-cache add ca-certificates

# aws-iam-authenticator lets a kubeconfig's exec credential plugin mint EKS
# tokens, which is what allows the runner to drive a cluster it is not running
# in. See docs/cross-cluster.md.
#
# A standalone static binary rather than the AWS CLI, which has no musl build,
# and rather than the AWS SDK in Go, which would make a deliberately
# cloud-agnostic binary AWS-aware. This way EKS support is a property of the
# image and the runner keeps speaking plain kubeconfig.
FROM alpine@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b AS authenticator
ARG TARGETARCH
ARG AWS_IAM_AUTHENTICATOR_VERSION=0.7.20
RUN apk --no-cache add curl && \
    case "${TARGETARCH}" in \
      amd64) sha=a60921d38d2b51bc998f37eb1eba81c92aed21d9315a35a01b283ac398c43f87 ;; \
      arm64) sha=8afd45da2ba288ee515e31215ab5a7247d92449c179281e0610522226c5e21a5 ;; \
      *) echo "unsupported TARGETARCH=${TARGETARCH}" >&2; exit 1 ;; \
    esac && \
    curl -fsSL -o /aws-iam-authenticator \
      "https://github.com/kubernetes-sigs/aws-iam-authenticator/releases/download/v${AWS_IAM_AUTHENTICATOR_VERSION}/aws-iam-authenticator_${AWS_IAM_AUTHENTICATOR_VERSION}_linux_${TARGETARCH}" && \
    echo "${sha}  /aws-iam-authenticator" | sha256sum -c - && \
    chmod +x /aws-iam-authenticator

FROM alpine:latest@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b
ARG TARGETPLATFORM
# copy the ca-certificate.crt from the build stage
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=authenticator /aws-iam-authenticator /usr/local/bin/aws-iam-authenticator
COPY ${TARGETPLATFORM}/opslevel-runner /opslevel-runner
ENTRYPOINT ["/opslevel-runner"]
