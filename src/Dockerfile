FROM ubuntu:focal AS release
ENV USER_UID=1001 USER_NAME=opslevel
ENTRYPOINT ["/usr/local/bin/opslevel-runner"]
WORKDIR /app
COPY opslevel-runner /usr/local/bin