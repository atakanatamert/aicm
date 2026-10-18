# Hub image: static Go binary on distroless, running as non-root.
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath \
      -ldflags "-s -w -X github.com/atakanatamert/aicm/internal/version.Version=${VERSION}" \
      -o /out/aicm ./cmd/aicm
RUN mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/aicm /usr/local/bin/aicm
COPY --from=build --chown=65532:65532 /out/data /var/lib/aicm
COPY LICENSE THIRD_PARTY_NOTICES.md /usr/share/doc/aicm/
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/aicm"]
CMD ["serve"]
