# syntax=docker/dockerfile:1
FROM rust:1-slim-bookworm AS build
WORKDIR /src

# deps compile once into their own layer; only src/ invalidates it
COPY Cargo.toml Cargo.lock ./
RUN --mount=type=cache,target=/usr/local/cargo/registry \
    mkdir src && echo 'fn main() {}' > src/main.rs && cargo build --release

COPY src ./src
RUN --mount=type=cache,target=/usr/local/cargo/registry \
    touch src/main.rs && cargo build --release

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates wget && \
    rm -rf /var/lib/apt/lists/*
WORKDIR /app
# read at startup; the executor refuses to start without it
COPY kuru-markets.json ./kuru-markets.json
COPY --from=build /src/target/release/executor /usr/local/bin/executor
ENV KURU_MARKETS_PATH=/app/kuru-markets.json EXECUTOR_ADDR=0.0.0.0:8083
EXPOSE 8083
USER nobody
ENTRYPOINT ["/usr/local/bin/executor"]
