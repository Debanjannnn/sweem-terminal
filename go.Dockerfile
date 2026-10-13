# one image per go service: --build-arg SVC=publisher
# compiles on the build machine's arch, cross-targets TARGETARCH (no emulation)
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS build
ARG SVC TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOARCH=$TARGETARCH go build -trimpath -o /out/app ./services/${SVC}/cmd/${SVC}

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/app /app
COPY --from=build /src/deployments /deployments
ENTRYPOINT ["/app"]
