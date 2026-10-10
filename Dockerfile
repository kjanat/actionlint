ARG GOLANG_VER=1.27.2
ARG ALPINE_VER=3.24

FROM golang:${GOLANG_VER} AS builder
WORKDIR /go/src/app
COPY go.* *.go sarif_template.txt ./
COPY cmd cmd/
COPY internal internal/
ENV CGO_ENABLED=0
ARG ACTIONLINT_VER=
RUN go build -v -ldflags "-s -w -X actionlint.kjanat.dev.version=${ACTIONLINT_VER} -X 'actionlint.kjanat.dev.installedFrom=official Docker image'" -o . ./cmd/actionlint

FROM koalaman/shellcheck-alpine:stable AS shellcheck
# Release binaries support larger ARM64 page sizes, unlike Ruff's distroless build.
FROM alpine:${ALPINE_VER} AS ruff
ARG TARGETARCH
RUN case "${TARGETARCH}" in \
      amd64) target=x86_64-unknown-linux-musl; checksum=0df9421fd0df9aa7a62dab4d1086e085b7c7888b6343fc5a9c5f630771d61aae ;; \
      arm64) target=aarch64-unknown-linux-musl; checksum=cac779097e34bf1ca40175b4bacf8fca7a92fce5030bfc3edf1fd357428a33b0 ;; \
      *) echo "Unsupported Ruff image architecture: ${TARGETARCH}" >&2; exit 1 ;; \
    esac \
    && wget -q "https://github.com/astral-sh/ruff/releases/download/0.17.0/ruff-${target}.tar.gz" -O /tmp/ruff.tar.gz \
    && printf '%s  /tmp/ruff.tar.gz\n' "${checksum}" > /tmp/ruff.sha256 \
    && sha256sum -c /tmp/ruff.sha256 \
    && tar -xzf /tmp/ruff.tar.gz -C /tmp \
    && cp "/tmp/ruff-${target}/ruff" /ruff \
    && /ruff --version

FROM alpine:${ALPINE_VER} AS runtime
LABEL org.opencontainers.image.source="https://github.com/kjanat/actionlint"
LABEL org.opencontainers.image.licenses="MIT"
COPY --from=builder /go/src/app/actionlint /usr/local/bin/
COPY --from=shellcheck /bin/shellcheck /usr/local/bin/shellcheck
COPY --from=ruff /ruff /usr/local/bin/ruff

FROM runtime AS action
COPY --chmod=755 scripts/docker-action.sh /usr/local/bin/actionlint-action
ENTRYPOINT ["/usr/local/bin/actionlint-action"]

FROM runtime AS cli
WORKDIR /w
USER 405
ENTRYPOINT ["/usr/local/bin/actionlint"]
