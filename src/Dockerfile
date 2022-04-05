FROM golang:alpine as build
RUN apk --no-cache add ca-certificates

FROM scratch
# copy the ca-certificate.crt from the build stage
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY opslevel-runner /opslevel-runner
ENTRYPOINT ["/opslevel-runner"]
