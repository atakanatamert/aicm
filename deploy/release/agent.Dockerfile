# Agent image built by GoReleaser from the prebuilt binary (task 30). Mount
# the host home read-only and set AICM_AGENT_HOME to it.
FROM gcr.io/distroless/static-debian12:nonroot
ARG TARGETPLATFORM
COPY $TARGETPLATFORM/aicm-agent /usr/local/bin/aicm-agent
COPY LICENSES/Apache-2.0.txt THIRD_PARTY_NOTICES.md /usr/share/doc/aicm-agent/
ENV AICM_AGENT_DATA=/home/nonroot/.config/aicm-agent
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/aicm-agent"]
CMD ["run"]
