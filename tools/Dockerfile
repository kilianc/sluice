# tools/Dockerfile — pinned toolchain for this project.
#
# @sluice/core has zero runtime dependencies and no build step, so this image
# installs nothing beyond the runtime itself: there is no npm install here, and
# therefore no postinstall script from anyone's dependency tree.
#
# Build:
#   docker build -t sluice-tools tools/
# Run the JS conformance adapter:
#   make conformance-js
FROM node:22.12.0-bookworm-slim

ENV NPM_CONFIG_UPDATE_NOTIFIER=false \
    NPM_CONFIG_FUND=false

WORKDIR /work
