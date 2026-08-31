# syntax=docker/dockerfile:1

ARG GO_VER=1.27.0

FROM golang:${GO_VER}-alpine AS build
WORKDIR /src

ENV GOTOOLCHAIN=local \
  CGO_ENABLED=0

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
  --mount=type=cache,target=/root/.cache/go-build \
  GOFLAGS=-mod=mod go run github.com/a-h/templ/cmd/templ generate && \
  go build -trimpath -ldflags="-s -w" -buildvcs=false -o /app ./cmd/app

# FROM scratch
FROM gcr.io/distroless/static-debian13:nonroot
LABEL org.opencontainers.image.title="ginvoice" \
  org.opencontainers.image.description="Self-hosted invoicing app"

COPY --from=build /app /app

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD ["/app", "healthcheck"]

ENTRYPOINT ["/app"]
