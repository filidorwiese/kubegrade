FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
    -o /kubegrade-agent ./cmd/kubegrade-agent

FROM gcr.io/distroless/static:nonroot
COPY --from=build /kubegrade-agent /kubegrade-agent
USER 65532:65532
ENTRYPOINT ["/kubegrade-agent"]
