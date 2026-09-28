# one image per go service: --build-arg SVC=publisher
FROM golang:1.25-alpine AS build
ARG SVC
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -o /out/app ./services/${SVC}/cmd/${SVC} && mkdir -p contracts/deployments

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/app /app
COPY --from=build /src/contracts/deployments /deployments
ENTRYPOINT ["/app"]
