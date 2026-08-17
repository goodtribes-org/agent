# --platform=$BUILDPLATFORM keeps the toolchain running natively on the build
# host; Go then cross-compiles to $TARGETARCH. Emulating the whole build under
# QEMU instead would be an order of magnitude slower for no benefit, since Go
# cross-compiles cleanly with CGO disabled.
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /src

ENV CGO_ENABLED=0

# Copy the manifests first so dependency download is cached separately from
# the source. go.sum does not exist — the module has no dependencies — so the
# glob keeps COPY from failing on a missing file.
COPY go.mod go.su[m] ./
RUN go mod download

COPY . .

RUN GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath -ldflags="-s -w" -o /out/ ./cmd/...

# ---

FROM alpine:3.20

# ca-certificates is not optional here: every outbound call is HTTPS to
# api.github.com or api.berget.ai.
RUN apk --no-cache add ca-certificates tzdata \
 && adduser -D -u 10001 organizzer

COPY --from=builder /out/ /usr/local/bin/

USER organizzer

# One image, three stages. The Deployment picks which one to run:
#   command: ["organizzer", "plan"]
ENTRYPOINT []
CMD ["organizzer", "request"]
