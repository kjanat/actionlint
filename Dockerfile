ARG GOLANG_VER=1.27.0
ARG ALPINE_VER=3.24

FROM golang:${GOLANG_VER} AS builder
WORKDIR /go/src/app
COPY go.* *.go ./
COPY cmd cmd/
ENV CGO_ENABLED=0
ARG ACTIONLINT_VER=
RUN go build -v -ldflags "-s -w -X github.com/kjanat/actionlint.version=${ACTIONLINT_VER}" -o . ./cmd/actionlint ./cmd/actionlint-action

FROM koalaman/shellcheck-alpine:stable AS shellcheck

FROM alpine:${ALPINE_VER} AS runtime
COPY --from=builder /go/src/app/actionlint /usr/local/bin/
COPY --from=shellcheck /bin/shellcheck /usr/local/bin/shellcheck
RUN apk add --no-cache python3 py3-pyflakes

FROM runtime AS action
COPY --from=builder /go/src/app/actionlint-action /usr/local/bin/
ENTRYPOINT ["/usr/local/bin/actionlint-action"]

FROM runtime AS cli
WORKDIR /w
USER 405
ENTRYPOINT ["/usr/local/bin/actionlint"]
