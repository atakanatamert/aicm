# Hub image built by GoReleaser from the prebuilt binary (task 30).
FROM busybox:1.37-musl AS prep
RUN mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot
ARG TARGETPLATFORM
COPY $TARGETPLATFORM/aicm /usr/local/bin/aicm
COPY LICENSE THIRD_PARTY_NOTICES.md /usr/share/doc/aicm/
# The master key file is generated here on first start (compose volume).
COPY --from=prep --chown=65532:65532 /out/data /var/lib/aicm
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/aicm"]
CMD ["serve"]
