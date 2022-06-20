#0 ----------------------------
FROM golang:1.18 as builder
WORKDIR /go/src/caihcloud-scheduler-framwork
COPY . /go/src/caihcloud-scheduler-framwork

ENV GOPROXY=https://goproxy.cn,direct
ENV PATH $GOPATH/bin:$PATH

RUN GO111MODULE="on" go build -o caihcloud-scheduler ./main.go && \
    chmod -R 777 caihcloud-scheduler

#1 ----------------------------
FROM debian:stretch-slim
COPY --from=builder /go/src/caihcloud-scheduler-framwork/caihcloud-scheduler /usr/local/bin/caihcloud-scheduler
CMD ["/usr/local/bin/caihcloud-scheduler"]