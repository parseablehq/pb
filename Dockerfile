FROM ubuntu:22.04
RUN apt-get -y update && apt install -y ca-certificates
WORKDIR /app
COPY pb .
ENTRYPOINT [ "./pb" ]
