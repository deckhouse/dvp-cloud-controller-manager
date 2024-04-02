FROM golang:1.21-alpine3.18 as builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /app
COPY go.mod go.mod
COPY go.sum go.sum

RUN go mod download

COPY cmd/ cmd/
COPY pkg/ pkg/

ENV CGO_ENABLED=0
ENV GOOS=${TARGETOS:-linux}
ENV GOARCH=${TARGETARCH:-amd64}

RUN go build -a -o dvp-cloud-controller-manager cmd/dvp-cloud-controller-manager/main.go


FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /app/dvp-cloud-controller-manager .
USER 65532:65532

ENTRYPOINT ["/dvp-cloud-controller-manager"]
