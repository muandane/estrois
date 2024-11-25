ARG GO_VERSION=1.23.3

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION} AS build

WORKDIR /src

RUN --mount=type=cache,target=/go/pkg/mod/ \
    --mount=type=bind,source=go.sum,target=go.sum \
    --mount=type=bind,source=go.mod,target=go.mod \
    go mod download -x

ARG TARGETARCH

RUN --mount=type=cache,target=/go/pkg/mod/ \
    --mount=type=bind,target=. \
    CGO_ENABLED=0 GOARCH=$TARGETARCH go build -ldflags="-s -w" -o /bin/estrois ./cmd/server/main.go

FROM cgr.dev/chainguard/static:latest AS final

LABEL maintainer="muandane"
USER nonroot:nonroot

COPY --from=build --chown=nonroot:nonroot /bin/estrois /bin/
EXPOSE 8080
ENTRYPOINT [ "/bin/estrois" ]
