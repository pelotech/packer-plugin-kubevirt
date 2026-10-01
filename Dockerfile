# Holds the plugin the way Packer expects it in its plugin directory, to be copied into another image
FROM --platform=$BUILDPLATFORM busybox:1.38.0 AS plugin
ARG TARGETPLATFORM
WORKDIR /plugins/github.com/pelotech/kubevirt
COPY $TARGETPLATFORM/packer-plugin-kubevirt_* ./
RUN for binary in packer-plugin-kubevirt_*; do printf '%s' "$(sha256sum "$binary" | cut -d ' ' -f 1)" > "${binary}_SHA256SUM"; done

FROM scratch
COPY --from=plugin /plugins /
