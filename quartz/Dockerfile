# syntax=docker/dockerfile:1.7

FROM node:22-bookworm-slim

RUN apt-get update && \
    apt-get install -y --no-install-recommends git ca-certificates && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Pin to a known-good v4 release. Bump intentionally when ready.
ARG QUARTZ_REF=v4.5.2
RUN git clone --depth 1 --branch ${QUARTZ_REF} https://github.com/jackyzha0/quartz.git . && \
    npm ci

# Our config overrides the template's quartz.config.ts.
COPY quartz.config.ts /app/quartz.config.ts
COPY build-loop.sh /usr/local/bin/build-loop.sh
RUN chmod +x /usr/local/bin/build-loop.sh

ENTRYPOINT ["/usr/local/bin/build-loop.sh"]
