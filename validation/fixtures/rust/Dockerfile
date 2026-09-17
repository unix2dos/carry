FROM rust:1.89-bookworm@sha256:948f9b08a66e7fe01b03a98ef1c7568292e07ec2e4fe90d88c07bb14563c84ff AS build
ARG TARGETPLATFORM
WORKDIR /src
COPY Cargo.toml Cargo.lock ./
COPY src ./src
RUN --mount=type=cache,id=pdv-rust-registry,target=/usr/local/cargo/registry,sharing=locked --mount=type=cache,id=pdv-rust-target-${TARGETPLATFORM},target=/src/target,sharing=locked cargo build --release --locked && cp target/release/personal-deploy-validation-rust /server

FROM debian:bookworm-slim@sha256:88200866dfff7ea7f5cbcb6ec7c8a701889efe6fe859fe64d6990e4b07ea4171
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates libssl3 && rm -rf /var/lib/apt/lists/*
COPY --from=build /server /server
USER 65532:65532
EXPOSE 8080
CMD ["/server"]
