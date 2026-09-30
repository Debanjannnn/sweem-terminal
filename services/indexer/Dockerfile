# build context: repo root
FROM rust:1-slim-bookworm AS build
WORKDIR /src
COPY services/indexer services/indexer
RUN cargo build --release --locked --manifest-path services/indexer/Cargo.toml

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
COPY --from=build /src/services/indexer/target/release/indexer /usr/local/bin/indexer
COPY contracts/deployments /deployments
EXPOSE 8085
CMD ["indexer"]
