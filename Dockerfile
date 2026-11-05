FROM golang:alpine@sha256:d200d2dd76b03a99b52651fd1325aff7dcd35422c35c01bac56571b0507f3deb AS build
RUN apk --no-cache add ca-certificates

FROM alpine:latest@sha256:be2656061b536c1883c4034790e21d0a09c5aaaf51968b89368a3c0371ea2c9a
# alpine:latest is only rebuilt on alpine point releases, so it can ship packages
# with CVEs that are already patched in the apk repos - pull those fixes in here
RUN apk --no-cache upgrade
ARG TARGETPLATFORM
# copy the ca-certificate.crt from the build stage
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY ${TARGETPLATFORM}/opslevel-runner /opslevel-runner
ENTRYPOINT ["/opslevel-runner"]
