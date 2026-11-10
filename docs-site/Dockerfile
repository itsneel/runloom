FROM node:26.13.0-alpine@sha256:8996bb5821d62a8fd9bb27c1cb07462f3fdce6e47d640c6a881ead0b064ce216 AS build
WORKDIR /app
ENV NG_CLI_ANALYTICS=false
RUN chown node:node /app
USER node
COPY --chown=node:node package.json package-lock.json ./
RUN npm ci --ignore-scripts --no-audit --no-fund
COPY --chown=node:node angular.json tsconfig.json tsconfig.app.json site.config.json LICENSE ./
COPY --chown=node:node src ./src
COPY --chown=node:node public ./public
COPY --chown=node:node content ./content
COPY --chown=node:node scripts/build-content.mjs scripts/markdown.mjs ./scripts/
RUN --network=none npm run build -- --configuration production --source-map=false

FROM nginx:1.30.6-alpine-slim@sha256:3028fc1419f99b95c1c69f844bbaf49dc1b881750c51b497fc721ac4f02a14e6
LABEL org.opencontainers.image.licenses="MIT"
COPY --chmod=0444 nginx-main.conf /etc/nginx/nginx.conf
COPY --chmod=0444 nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist/runloom-docs/browser /usr/share/nginx/html
USER 101:101
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=3 CMD wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["nginx", "-g", "daemon off;"]
