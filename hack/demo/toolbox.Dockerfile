# The toolbox of the local demo (docker-compose.yaml): the Docker CLI, kind, kubectl and the Vault CLI. It creates the
# kind cluster through the host's Docker socket and keeps Vault, the cluster and the sign-in stand-in configured.
FROM hashicorp/vault:2.2.1@sha256:962a92876e168d68eaaa24b0b8b49bbce1cb63024060a67bba95cd158a366fc2 AS vault

FROM docker:29-cli
ARG TARGETARCH
# The versions of hack/install-tools.sh.
ARG KIND_VERSION=v0.33.0
ARG KUBECTL_VERSION=v1.37.2
RUN apk add --no-cache bash curl jq
RUN curl -fsSLo /usr/local/bin/kind "https://github.com/kubernetes-sigs/kind/releases/download/${KIND_VERSION}/kind-linux-${TARGETARCH}" \
 && echo "$(curl -fsSL "https://github.com/kubernetes-sigs/kind/releases/download/${KIND_VERSION}/kind-linux-${TARGETARCH}.sha256sum" | cut -d' ' -f1)  /usr/local/bin/kind" | sha256sum -c - \
 && curl -fsSLo /usr/local/bin/kubectl "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${TARGETARCH}/kubectl" \
 && echo "$(curl -fsSL "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${TARGETARCH}/kubectl.sha256")  /usr/local/bin/kubectl" | sha256sum -c - \
 && chmod +x /usr/local/bin/kind /usr/local/bin/kubectl
COPY --from=vault /bin/vault /usr/local/bin/vault
COPY toolbox.sh kind.yaml manifests.yaml /demo/
ENTRYPOINT ["/demo/toolbox.sh"]
