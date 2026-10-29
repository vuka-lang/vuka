# The playground runner: build from the repository's root.
#
#	docker build -f playground/runner/Dockerfile -t vuka-runner .
#	docker run --privileged --cgroupns=private -p 8090:8090 vuka-runner
ARG GO_VERSION=1.27.3

FROM golang:${GO_VERSION}-bookworm AS nsjail
RUN apt-get update && apt-get install -y --no-install-recommends \
      autoconf bison flex gcc g++ git libprotobuf-dev libnl-route-3-dev libtool make pkg-config protobuf-compiler \
    && rm -rf /var/lib/apt/lists/*
ARG NSJAIL_VERSION=3.4
RUN git clone --depth 1 --branch ${NSJAIL_VERSION} https://github.com/google/nsjail /nsjail \
    && make -C /nsjail -j"$(nproc)" && strip /nsjail/nsjail

FROM golang:${GO_VERSION}-bookworm AS tools
WORKDIR /src
COPY . .
RUN go work init . ./cmd/vuka \
    && CGO_ENABLED=0 go build -trimpath -ldflags=-s -o /out/vuka ./cmd/vuka \
    && cd playground/runner && CGO_ENABLED=0 GOWORK=off go build -trimpath -ldflags=-s -o /out/runner . \
    && cd /src && rm -f go.work go.work.sum \
    && mkdir /out/vuka-src && cp -a . /out/vuka-src \
    && find /out/vuka-src -mindepth 2 -name go.mod -printf '%h\n' | xargs rm -rf \
    && rm -rf /out/vuka-src/playground /out/vuka-src/editors

FROM golang:${GO_VERSION}-bookworm
RUN apt-get update && apt-get install -y --no-install-recommends libprotobuf32 libnl-route-3-200 \
    && rm -rf /var/lib/apt/lists/*
ARG TAILWIND_VERSION=v4.3.0
ADD --chmod=755 https://github.com/tailwindlabs/tailwindcss/releases/download/${TAILWIND_VERSION}/tailwindcss-linux-x64 /usr/local/bin/tailwindcss
COPY --from=nsjail /nsjail/nsjail /usr/local/bin/nsjail
COPY --from=tools /out/vuka /out/runner /usr/local/bin/
COPY --from=tools /out/vuka-src /opt/vuka
COPY docs/testdata/gomode/go.mod docs/testdata/gomode/go.sum /opt/play/gomode/
COPY docs/playground/examples /opt/play/examples
ENV RUNNER_GOMOD=/opt/play/gomode/go.mod \
    RUNNER_VUKA_SRC=/opt/vuka \
    RUNNER_VUKA=/usr/local/bin/vuka \
    RUNNER_SHIM=/usr/local/bin/runner \
    RUNNER_NSJAIL=/usr/local/bin/nsjail \
    RUNNER_GOROOT=/usr/local/go \
    RUNNER_GOMODCACHE=/opt/gomodcache \
    RUNNER_GOCACHE=/opt/gocache \
    RUNNER_DIR=/var/lib/runner \
    RUNNER_SANDBOX=nsjail
# The module cache holds what the docs pin, nothing more; the build cache
# is warm from compiling every example as a sandboxed build does.
RUN mkdir /tmp/dl && cd /tmp/dl \
    && sed -e 's#^module .*#module example.com/play#' -e 's#=> \.\./\.\./\.\.#=> /opt/vuka#' -e '/^tool /d' /opt/play/gomode/go.mod > go.mod \
    && cp /opt/play/gomode/go.sum . \
    && GOMODCACHE=/opt/gomodcache GOFLAGS=-mod=mod go mod download \
    && cd / && rm -rf /tmp/dl \
    && GOMODCACHE=/opt/gomodcache GOCACHE=/opt/gocache runner warm /opt/play/examples/*
RUN chmod -R a+rX /opt/gomodcache /opt/vuka && chown -R 65534:65534 /opt/gocache && mkdir -p /var/lib/runner
EXPOSE 8090
CMD ["runner"]
