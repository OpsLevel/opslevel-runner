FROM golang:alpine@sha256:f92b6ef800e499660581efdabdf25d9d817a9d124eaf900924f0504e7e27e12d AS build
RUN apk --no-cache add ca-certificates

FROM alpine:latest@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b
# alpine:latest is only rebuilt on alpine point releases, so it can ship packages
# with CVEs that are already patched in the apk repos - pull those fixes in here
RUN apk --no-cache upgrade
ARG TARGETPLATFORM
# copy the ca-certificate.crt from the build stage
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY ${TARGETPLATFORM}/opslevel-runner /opslevel-runner
ENTRYPOINT ["/opslevel-runner"]
