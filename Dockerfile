# syntax=docker/dockerfile:1.7
FROM golang:1.27-bookworm AS builder

WORKDIR /workspace

# Copy local modules and vendor tree
COPY go.mod go.sum ./
COPY vendor/ vendor/
COPY api/ api/
COPY cmd/ cmd/
COPY internal/ internal/

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -mod=vendor -trimpath -ldflags="-s -w" -o /out/manager ./cmd/manager

FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /out/manager /manager
USER 65532:65532

EXPOSE 8080 8081 9443
ENTRYPOINT ["/manager"]
